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
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Listen, Token, SourceRoot, RestoreRoot, MobileIngestRoot, KopiaBin, RcloneBin, WebDir string
}
type Handler struct {
	cfg  Config
	root string
	// Only one expensive repository-writing/verification operation per instance.
	jobGate     chan struct{}
	remoteGate  chan struct{}
	restoreRoot string
	mobileRoot  string
	webDir      string
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Listen:           value("GCB_LISTEN", "127.0.0.1:8787"),
		Token:            os.Getenv("GCB_API_TOKEN"),
		SourceRoot:       os.Getenv("GCB_SOURCE_ROOT"),
		RestoreRoot:      os.Getenv("GCB_RESTORE_ROOT"),
		MobileIngestRoot: os.Getenv("GCB_MOBILE_INGEST_ROOT"),
		KopiaBin:         value("GCB_KOPIA_BIN", "kopia"),
		RcloneBin:        value("GCB_RCLONE_BIN", "rclone"),
		WebDir:           os.Getenv("GCB_WEB_DIR"),
	}
	if len(cfg.Token) < 32 {
		return Config{}, errors.New("GCB_API_TOKEN must be at least 32 bytes; no default credentials")
	}
	if cfg.SourceRoot == "" {
		return Config{}, errors.New("GCB_SOURCE_ROOT is required")
	}
	if err := validateLocalListen(cfg.Listen); err != nil {
		return Config{}, err
	}
	// Deliberately bind to loopback; expose through a separately secured TLS proxy/VPN only.
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
	if cfg.Listen != "" {
		if err := validateLocalListen(cfg.Listen); err != nil {
			return nil, err
		}
	}
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
	restoreRoot, err := validateRestoreRoot(root, cfg.RestoreRoot)
	if err != nil {
		return nil, err
	}
	webDir := ""
	if cfg.WebDir != "" {
		webDir, err = filepath.EvalSymlinks(cfg.WebDir)
		if err != nil {
			return nil, fmt.Errorf("web root: %w", err)
		}
		webStat, statErr := os.Stat(webDir)
		if statErr != nil || !webStat.IsDir() {
			return nil, errors.New("web root must be an existing directory")
		}
		indexPath, indexErr := filepath.EvalSymlinks(filepath.Join(webDir, "index.html"))
		if indexErr != nil || !pathInside(webDir, indexPath) {
			return nil, errors.New("web root must contain an internal index.html")
		}
	}
	mobileRoot, err := validateMobileRoot(root, cfg.MobileIngestRoot)
	if err != nil {
		return nil, err
	}
	h := &Handler{cfg: cfg, root: root, restoreRoot: restoreRoot, mobileRoot: mobileRoot, webDir: webDir, jobGate: make(chan struct{}, 1), remoteGate: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /api/v1/status", h.authorize(h.status))
	mux.HandleFunc("GET /api/v1/snapshots", h.authorize(h.snapshots))
	mux.HandleFunc("GET /api/v1/remotes", h.authorize(h.remotes))
	mux.HandleFunc("GET /api/v1/remotes/about", h.authorize(h.remoteCapacity))
	mux.HandleFunc("POST /api/v1/snapshots", h.authorize(h.create))
	mux.HandleFunc("POST /api/v1/mobile/files", h.authorize(h.mobileFile))
	mux.HandleFunc("POST /api/v1/verify", h.authorize(h.verify))
	mux.HandleFunc("POST /api/v1/restore", h.authorize(h.restore))
	mux.HandleFunc("GET /api/v1/info", h.authorize(h.info))
	if h.webDir != "" {
		mux.Handle("/", http.HandlerFunc(h.serveWeb))
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
		// Refuse ambiguous duplicate Authorization headers, even when a proxy
		// could interpret or concatenate them differently than net/http.
		values := r.Header.Values("Authorization")
		const prefix = "Bearer "
		if len(values) != 1 || !strings.HasPrefix(values[0], prefix) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, http.StatusUnauthorized, "authorization required")
			return
		}
		presented := strings.TrimPrefix(values[0], prefix)
		if len(presented) != len(h.cfg.Token) || subtle.ConstantTimeCompare([]byte(presented), []byte(h.cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, http.StatusUnauthorized, "authorization required")
			return
		}
		next(w, r)
	}
}

func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		writeErr(w, http.StatusUnsupportedMediaType, "exactly one application/json Content-Type header is required")
		return false
	}
	media, _, err := mime.ParseMediaType(values[0])
	if err != nil || media != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, "application/json Content-Type required")
		return false
	}
	return true
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
	respond(w, 200, map[string]any{"name": "GoreeCloud Backup", "version": "0.1.0-dev", "features": []string{"kopia-status", "snapshot-list", "snapshot-create", "rclone-remotes", "rclone-remote-capacity", "full-file-verify", "contained-restore"}, "sourceRoot": h.root, "restoreEnabled": h.restoreRoot != "" && restoreSandboxAvailable(), "mobileUploadEnabled": h.mobileRoot != ""})
}
func run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	// Never leak the API bearer secret to separately executed upstream programs.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GCB_API_TOKEN=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
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
func (h *Handler) beginJob(w http.ResponseWriter) bool {
	select {
	case h.jobGate <- struct{}{}:
		return true
	default:
		writeErr(w, http.StatusConflict, "a repository operation is already in progress")
		return false
	}
}

// verify performs a complete upstream encrypted-content read/verification. It
// deliberately has no user-supplied command arguments and cannot modify data.
// This is an expensive, explicit administrator action; it is not a restore drill.
func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	if !h.beginJob(w) {
		return
	}
	defer func() { <-h.jobGate }()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	output, err := run(ctx, h.cfg.KopiaBin, "snapshot", "verify", "--verify-files-percent=100", "--json")
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Kopia full-content verification failed or timed out; inspect local service logs")
		return
	}
	var result struct {
		Stats *struct {
			ProcessedObjectCount     int64 `json:"processedObjectCount"`
			ExpectedTotalObjectCount int64 `json:"expectedTotalObjectCount"`
			ReadFileCount            int64 `json:"readFileCount"`
			ExpectedTotalFileCount   int64 `json:"expectedTotalFileCount"`
			ReadBytes                int64 `json:"readBytes"`
		} `json:"stats"`
		ErrorCount *int64 `json:"errorCount"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Stats == nil || result.ErrorCount == nil {
		writeErr(w, http.StatusBadGateway, "unexpected Kopia verification response")
		return
	}
	stats := result.Stats
	// Kopia deduplicates identical file/object contents across snapshots. The
	// number of unique objects it reads can be lower than the total reference
	// count; comparing these numbers for equality incorrectly reports failure.
	if *result.ErrorCount != 0 || stats.ReadFileCount < 1 || stats.ExpectedTotalFileCount < 1 || stats.ProcessedObjectCount < 1 {
		writeErr(w, http.StatusBadGateway, "incomplete or failed Kopia file verification; investigate repository integrity")
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"verified": true, "filesRead": stats.ReadFileCount, "bytesRead": stats.ReadBytes,
		"fileReferences": stats.ExpectedTotalFileCount, "objectsRead": stats.ProcessedObjectCount,
		"scope": "100% of files in the currently available snapshots",
		"note":  "An independent restore test is still required",
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
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
	if !h.beginJob(w) {
		return
	}
	defer func() { <-h.jobGate }()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	_, err = run(ctx, h.cfg.KopiaBin, "snapshot", "create", source)
	if err != nil {
		writeErr(w, 502, "Kopia backup failed; consult local service logs")
		return
	}
	respond(w, 201, map[string]any{"accepted": true, "source": payload.Source, "message": "Kopia snapshot command completed"})
}
