// Package backup exposes a limited API around a locally configured Kopia CLI.
// Copyright 2026 GoreeCloud. Licensed under Apache-2.0.
package backup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Listen, Token, SourceRoot, KopiaBin, RcloneBin, WebDir string
}
type Handler struct {
	cfg  Config
	root string
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Listen:     value("GCB_LISTEN", "127.0.0.1:8787"),
		Token:      os.Getenv("GCB_API_TOKEN"),
		SourceRoot: os.Getenv("GCB_SOURCE_ROOT"),
		KopiaBin:   value("GCB_KOPIA_BIN", "kopia"),
		RcloneBin:  value("GCB_RCLONE_BIN", "rclone"),
		WebDir:     os.Getenv("GCB_WEB_DIR"),
	}
	if len(cfg.Token) < 32 {
		return Config{}, errors.New("GCB_API_TOKEN must be at least 32 bytes; no default credentials")
	}
	if cfg.SourceRoot == "" {
		return Config{}, errors.New("GCB_SOURCE_ROOT is required")
	}
	// Deliberately default to loopback; expose through a separately secured TLS proxy/VPN only.
	return cfg, nil
}
func value(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
func NewHandler(cfg Config) (http.Handler, error) {
	if len(cfg.Token) < 32 {
		return nil, errors.New("token must be at least 32 bytes")
	}
	if cfg.SourceRoot == "" {
		return nil, errors.New("missing source root")
	}
	root, err := filepath.EvalSymlinks(cfg.SourceRoot)
	if err != nil {
		return nil, fmt.Errorf("source root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, errors.New("source root must be an existing directory")
	}
	h := &Handler{cfg: cfg, root: root}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /api/v1/status", h.authorize(h.status))
	mux.HandleFunc("GET /api/v1/snapshots", h.authorize(h.snapshots))
	mux.HandleFunc("GET /api/v1/remotes", h.authorize(h.remotes))
	mux.HandleFunc("POST /api/v1/snapshots", h.authorize(h.create))
	mux.HandleFunc("GET /api/v1/info", h.authorize(h.info))
	if cfg.WebDir != "" {
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && r.Method != "HEAD" {
				http.Error(w, "method not allowed", 405)
				return
			}
			path := filepath.Join(cfg.WebDir, strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/"))
			if _, err := os.Stat(path); err == nil {
				http.ServeFile(w, r, path)
				return
			}
			http.ServeFile(w, r, filepath.Join(cfg.WebDir, "index.html"))
		}))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	}), nil
}
func (h *Handler) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		ok := strings.HasPrefix(auth, prefix) && len(auth)-len(prefix) == len(h.cfg.Token) && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(h.cfg.Token)) == 1
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, 401, "authorization required")
			return
		}
		next(w, r)
	}
}
func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func writeErr(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}
func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]any{"name": "GoreeCloud Backup", "version": "0.1.0-dev", "features": []string{"kopia-status", "snapshot-list", "snapshot-create", "rclone-remotes"}, "sourceRoot": h.root})
}
func run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	// Bounded output and no shell interpolation. Never echo command environment.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(stdout, (2<<20)+1))
	if readErr != nil || len(body) > (2<<20) {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if len(body) > (2 << 20) {
		return nil, errors.New("command output exceeded 2 MiB")
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return body, nil
}
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	output, err := run(ctx, h.cfg.KopiaBin, "repository", "status")
	if err != nil {
		respond(w, 200, map[string]any{"connected": false, "message": "Kopia repository unavailable or not connected"})
		return
	}
	s := strings.TrimSpace(string(output))
	if len(s) > 4096 {
		s = s[:4096]
	}
	respond(w, 200, map[string]any{"connected": true, "message": s})
}
func (h *Handler) snapshots(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	output, err := run(ctx, h.cfg.KopiaBin, "snapshot", "list", "--json")
	if err != nil {
		writeErr(w, 503, "unable to list Kopia snapshots")
		return
	}
	if !json.Valid(output) {
		writeErr(w, 502, "unexpected Kopia output")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(output)
}
func (h *Handler) remotes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	output, err := run(ctx, h.cfg.RcloneBin, "listremotes")
	if err != nil {
		writeErr(w, 503, "Rclone is unavailable")
		return
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	remotes := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && len(line) < 128 {
			remotes = append(remotes, line)
		}
	}
	respond(w, 200, map[string]any{"remotes": remotes})
}
func (h *Handler) safeSource(input string) (string, error) {
	if input == "" || filepath.IsAbs(input) {
		return "", errors.New("source must be a relative directory below the configured root")
	}
	for _, part := range strings.Split(filepath.ToSlash(input), "/") {
		if part == ".." || part == "." || part == "" {
			return "", errors.New("invalid source path")
		}
	}
	candidate := filepath.Join(h.root, input)
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", errors.New("source directory does not exist")
	}
	relative, err := filepath.Rel(h.root, real)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("source must remain within configured root")
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return "", errors.New("source must be a directory")
	}
	return real, nil
}
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	var payload struct {
		Source string `json:"source"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeErr(w, 400, "unexpected trailing data")
		return
	}
	source, err := h.safeSource(payload.Source)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	_, err = run(ctx, h.cfg.KopiaBin, "snapshot", "create", source)
	if err != nil {
		writeErr(w, 502, "Kopia backup failed; consult local service logs")
		return
	}
	respond(w, 201, map[string]any{"accepted": true, "source": payload.Source, "message": "Kopia snapshot command completed"})
}
