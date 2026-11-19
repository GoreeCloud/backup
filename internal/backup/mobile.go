package backup

import (
	"context"
	"crypto/rand"
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

// openPrivateMobileRoot pins access to the original staging directory handle,
// then compares that handle against both the configured path and its startup
// identity. A symlink or private directory substituted at the same path is
// not an acceptable replacement. os.Root operations cannot traverse outside
// the opened tree even if the containing parent changes later.
func (h *Handler) openPrivateMobileRoot() (*os.Root, error) {
	if h.mobileRoot == "" || h.mobileInfo == nil {
		return nil, errors.New("mobile ingest root is not configured")
	}
	root, err := os.OpenRoot(h.mobileRoot)
	if err != nil {
		return nil, errors.New("cannot open private ingest directory")
	}
	closeOnError := func(e error) (*os.Root, error) { _ = root.Close(); return nil, e }
	opened, err := root.Stat(".")
	if err != nil {
		return closeOnError(errors.New("cannot inspect private ingest directory"))
	}
	current, err := os.Lstat(h.mobileRoot)
	if err != nil || !current.IsDir() || current.Mode().Perm()&0077 != 0 ||
		!os.SameFile(opened, h.mobileInfo) || !os.SameFile(current, h.mobileInfo) {
		return closeOnError(errors.New("mobile ingest directory was substituted or permissions changed"))
	}
	directory, err := root.Open(".")
	if err != nil {
		return closeOnError(errors.New("cannot read private ingest root"))
	}
	entries, readErr := directory.ReadDir(1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return closeOnError(errors.New("cannot enumerate private ingest root"))
	}
	if closeErr != nil {
		return closeOnError(errors.New("cannot close private ingest root listing"))
	}
	if len(entries) != 0 {
		return closeOnError(errors.New("mobile ingest root contains unexpected files; operator inspection required"))
	}
	return root, nil
}

// cleanupMobileWorkspace removes only the one created file and directory.
// os.Root.RemoveAll requires Go >=1.25; this intentionally supports the
// repository's Go 1.24 minimum without falling back to unsafe absolute paths.
// Never delete a substituted workspace entry under the pinned parent.
func cleanupMobileWorkspace(parent, workspace *os.Root, workspaceName, filename string) error {
	if err := workspace.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot remove temporary upload file")
	}
	expected, err := workspace.Stat(".")
	if err != nil {
		return errors.New("cannot inspect private upload workspace")
	}
	current, err := parent.Lstat(workspaceName)
	if err != nil || !current.IsDir() || !os.SameFile(current, expected) {
		return errors.New("temporary upload workspace was substituted")
	}
	if err := parent.Remove(workspaceName); err != nil {
		return errors.New("cannot remove temporary upload workspace")
	}
	return nil
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
	stageRoot, err := h.openPrivateMobileRoot()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "private ingest directory identity or contents changed; operator review required")
		return
	}
	defer stageRoot.Close()
	// Allocate a private random child under the anchored root descriptor.
	// Never use an untrusted filesystem path for staging writes or cleanup.
	workspaceName := ""
	for attempts := 0; attempts < 3; attempts++ {
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			break
		}
		candidate := "mobile-" + hex.EncodeToString(nonce[:])
		err = stageRoot.Mkdir(candidate, 0700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err == nil {
			workspaceName = candidate
		}
		break
	}
	if workspaceName == "" {
		writeErr(w, http.StatusInternalServerError, "unable to allocate private mobile staging area")
		return
	}
	// Cleanup is anchored to the exact original directory even if the
	// configured absolute path is renamed while an upload is in progress.
	// Cleanup is registered after obtaining the child descriptor below.
	workspaceRoot, err := stageRoot.OpenRoot(workspaceName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unable to open private mobile staging area")
		return
	}
	defer workspaceRoot.Close()
	removed := false
	defer func() {
		if !removed {
			_ = cleanupMobileWorkspace(stageRoot, workspaceRoot, workspaceName, name)
		}
	}()
	workspace := filepath.Join(h.mobileRoot, workspaceName)
	file, err := workspaceRoot.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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

	// Kopia accepts a path (not an inherited directory handle). Refuse a
	// changed absolute path before passing it to the external process; this
	// does not fully eliminate a hostile same-UID rename race during Kopia's
	// own path traversal, so the parent must remain private.
	current, checkErr := os.Lstat(h.mobileRoot)
	child, childErr := stageRoot.Lstat(workspaceName)
	original, originalErr := workspaceRoot.Stat(".")
	if checkErr != nil || current == nil || !os.SameFile(current, h.mobileInfo) ||
		childErr != nil || originalErr != nil || !os.SameFile(child, original) {
		writeErr(w, http.StatusServiceUnavailable, "mobile staging path changed during ingestion")
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
	if err := cleanupMobileWorkspace(stageRoot, workspaceRoot, workspaceName, name); err != nil {
		writeErr(w, http.StatusInternalServerError, "encrypted snapshot completed but temporary plaintext cleanup requires attention")
		return
	}
	removed = true
	respond(w, http.StatusCreated, map[string]any{
		"backedUp": true, "name": name, "bytes": bytesCopied,
		"sha256": hex.EncodeToString(digest.Sum(nil)),
		"note":   "Kopia snapshot completed; independently verify and restore before relying on it",
	})
}
