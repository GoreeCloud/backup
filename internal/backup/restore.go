package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var objectIDFormat = regexp.MustCompile(`^[a-zA-Z0-9_+-]{8,160}$`)
var snapshotIDFormat = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

func pathInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// validateRestoreRoot prohibits backup/restore tree overlap and unsafe shared write access.
// The configured directory must already exist, must not be /, and is never created by the service.
func validateRestoreRoot(source, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("GCB_RESTORE_ROOT must be an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("restore root: %w", err)
	}
	if resolved == string(filepath.Separator) {
		return "", errors.New("restore root cannot be filesystem root")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("restore root must be an existing directory")
	}
	if info.Mode().Perm()&0022 != 0 {
		return "", errors.New("restore root must not be writable by other users or groups")
	}
	if pathInside(source, resolved) || pathInside(resolved, source) {
		return "", errors.New("restore root and source root must be separate, non-overlapping directories")
	}
	return resolved, nil
}

type snapshotRecord struct {
	ID        string `json:"id"`
	RootEntry struct {
		Obj string `json:"obj"`
	} `json:"rootEntry"`
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	if h.restoreRoot == "" {
		writeErr(w, http.StatusConflict, "restoration is disabled; configure a separate GCB_RESTORE_ROOT")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	var payload struct {
		SnapshotID  string `json:"snapshotId"`
		Acknowledge bool   `json:"acknowledge"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request JSON")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "unexpected trailing data")
		return
	}
	if !snapshotIDFormat.MatchString(payload.SnapshotID) || !payload.Acknowledge {
		writeErr(w, http.StatusBadRequest, "select a valid snapshot and explicitly acknowledge the restore")
		return
	}
	if !h.beginJob(w) {
		return
	}
	defer func() { <-h.jobGate }()

	ctxList, cancelList := context.WithTimeout(r.Context(), 30*time.Second)
	output, err := run(ctxList, h.cfg.KopiaBin, "snapshot", "list", "--json")
	cancelList()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "unable to query current Kopia snapshots")
		return
	}

	var records []snapshotRecord
	if err := json.Unmarshal(output, &records); err != nil {
		writeErr(w, http.StatusBadGateway, "unexpected Kopia snapshot response")
		return
	}
	var objectID string
	for _, record := range records {
		if record.ID == payload.SnapshotID {
			if objectID != "" {
				writeErr(w, http.StatusBadGateway, "duplicate snapshot identifier")
				return
			}
			objectID = record.RootEntry.Obj
		}
	}
	if objectID == "" {
		writeErr(w, http.StatusNotFound, "snapshot was not found in the current repository")
		return
	}
	// The root object ID comes from Kopia's list, not a caller-controlled CLI argument.
	if !objectIDFormat.MatchString(objectID) {
		writeErr(w, http.StatusBadGateway, "invalid Kopia root object identifier")
		return
	}

	// Recheck configured root before writing, to detect obvious permission or symlink changes.
	current, statErr := os.Lstat(h.restoreRoot)
	if statErr != nil || !current.IsDir() || current.Mode().Perm()&0022 != 0 {
		writeErr(w, http.StatusServiceUnavailable, "restore workspace is no longer a safe writable directory")
		return
	}

	// All output lives in a newly created private directory that cannot be selected by callers.
	// No restore path supplied by a client is ever accepted.
	parent, err := os.MkdirTemp(h.restoreRoot, "restore-")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot allocate isolated restore workspace")
		return
	}
	destination := filepath.Join(parent, "files")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	_, err = run(ctx, h.cfg.KopiaBin, "snapshot", "restore", objectID, destination,
		"--no-overwrite-files", "--no-overwrite-directories", "--no-overwrite-symlinks",
		"--skip-owners", "--skip-permissions")
	if err != nil {
		// Remove only our freshly allocated private workspace on failure.
		_ = os.RemoveAll(parent)
		writeErr(w, http.StatusBadGateway, "Kopia restore failed; inspect service logs and verify that the isolated workspace was removed")
		return
	}
	if auditErr := auditRecoveredTree(destination); auditErr != nil {
		_ = os.RemoveAll(parent)
		writeErr(w, http.StatusBadGateway, "recovered snapshot contains unsafe file types or links; quarantined copy removed")
		return
	}
	respond(w, http.StatusCreated, map[string]any{
		"restored":     true,
		"snapshotId":   payload.SnapshotID,
		"relativePath": filepath.ToSlash(filepath.Join(filepath.Base(parent), "files")),
		"note":         "Restore was isolated; verify contents and metadata before relying on recovered data",
	})
}
