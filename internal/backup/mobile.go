package backup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// One foreground-selected file per request, with a hard ceiling on plain
// ingress data. The caller never chooses the server staging path.
const maxMobileFileBytes int64 = 32 << 20

func validateMobileRoot(sourceRoot, configured string) (string, error) {
	if configured == "" {
		return "", nil
	}
	if !filepath.IsAbs(configured) {
		return "", errors.New("GCB_MOBILE_INGEST_ROOT must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(configured)
	if err != nil {
		return "", fmt.Errorf("mobile ingest root: %w", err)
	}
	// Uploads contain plaintext until Kopia has created the encrypted
	// snapshot. Keeping staging anywhere inside the normal source tree could
	// accidentally archive plaintext via a broad source snapshot or a crash.
	if resolved != filepath.Clean(configured) || resolved == string(filepath.Separator) ||
		pathInside(sourceRoot, resolved) || pathInside(resolved, sourceRoot) {
		return "", errors.New("mobile ingest root must be a real, separate, non-overlapping directory outside the source allowlist")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("mobile ingest root must be an existing private directory (0700)")
	}
	// A previous server crash could have left original plaintext bytes.
	// Never silently delete or ignore that data. The operator must inspect
	// and safely clear the private directory before starting the service.
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", errors.New("cannot inspect the private mobile ingest directory")
	}
	if len(entries) != 0 {
		return "", errors.New("mobile ingest root is not empty; operator must review leftover staging data before startup")
	}
	return resolved, nil
}
func safeMobileFilename(name string) bool {
	if name == "" || len(name) > 160 || !utf8.ValidString(name) ||
		name == "." || name == ".." || strings.ContainsAny(name, "/\\") ||
		strings.TrimSpace(name) != name {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func (h *Handler) mobileFile(w http.ResponseWriter, r *http.Request) {
	if h.mobileRoot == "" {
		writeErr(w, http.StatusConflict, "mobile selected-file backup is disabled; configure a private ingest directory")
		return
	}
	fileNames := r.Header.Values("X-GCB-Filename-B64")
	confirms := r.Header.Values("X-GCB-Confirm-Upload")
	media := r.Header.Values("Content-Type")
	name := ""
	if len(fileNames) == 1 && len(fileNames[0]) <= 256 {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(fileNames[0])
		if decodeErr == nil {
			name = string(decoded)
		}
	}
	if !safeMobileFilename(name) ||
		len(confirms) != 1 || confirms[0] != "true" || len(media) != 1 {
		writeErr(w, http.StatusBadRequest, "a valid filename, explicit consent and binary content type are required")
		return
	}
	mediaType, _, err := mime.ParseMediaType(media[0])
	if err != nil || mediaType != "application/octet-stream" {
		writeErr(w, http.StatusUnsupportedMediaType, "application/octet-stream is required")
		return
	}
	if r.ContentLength > maxMobileFileBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "selected file exceeds the 32 MiB limit")
		return
	}
	if !h.beginJob(w) {
		return
	}
	defer func() { <-h.jobGate }()
	current, err := os.Lstat(h.mobileRoot)
	if err != nil || !current.IsDir() || current.Mode().Perm()&0077 != 0 {
		writeErr(w, http.StatusServiceUnavailable, "mobile ingest workspace permissions changed")
		return
	}
	// Check again for leftover files before each upload. The process-local
	// job gate serializes legitimate writes, so unexpected directory entries
	// indicate another writer or crash residue that requires operator review.
	entries, err := os.ReadDir(h.mobileRoot)
	if err != nil || len(entries) != 0 {
		writeErr(w, http.StatusServiceUnavailable, "mobile ingest root contains unexpected files; operator inspection required")
		return
	}

	workspace, err := os.MkdirTemp(h.mobileRoot, "mobile-")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unable to allocate private mobile staging area")
		return
	}
	// Always remove the temporary plaintext copy, including on aborted uploads.
	defer func() { _ = os.RemoveAll(workspace) }()
	path := filepath.Join(workspace, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unable to stage selected file")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMobileFileBytes)
	digest := sha256.New()
	bytesCopied, copyErr := io.Copy(io.MultiWriter(file, digest), r.Body)
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(copyErr, &sizeErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "selected file exceeds the 32 MiB limit")
		} else {
			writeErr(w, http.StatusBadRequest, "unable to receive selected file")
		}
		return
	}
	// Do not snapshot a truncated body even if a proxy or test transport
	// incorrectly returned EOF without signaling a framing error.
	if r.ContentLength >= 0 && bytesCopied != r.ContentLength {
		writeErr(w, http.StatusBadRequest, "selected file length does not match request framing")
		return
	}
	if syncErr != nil || closeErr != nil {
		writeErr(w, http.StatusInternalServerError, "cannot safely persist temporary selected file")
		return
	}
	if bytesCopied == 0 {
		writeErr(w, http.StatusBadRequest, "empty selected file")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	_, err = run(ctx, h.cfg.KopiaBin, "snapshot", "create", workspace)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "encrypted Kopia snapshot creation failed; temporary staging cleanup attempted")
		return
	}
	// Fail closed if the staged plaintext cannot be removed.
	if err := os.RemoveAll(workspace); err != nil {
		writeErr(w, http.StatusInternalServerError, "encrypted snapshot completed but temporary plaintext cleanup requires attention")
		return
	}
	respond(w, http.StatusCreated, map[string]any{
		"backedUp": true, "name": name, "bytes": bytesCopied,
		"sha256": hex.EncodeToString(digest.Sum(nil)),
		"note":   "Kopia snapshot completed; independently verify and restore before relying on it",
	})
}
