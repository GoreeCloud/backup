package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// An Rclone remote is an already configured provider name ending in ":".
// This intentionally excludes flags, paths, and provider-specific options.
var safeRemoteName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}:$`)

// remoteCapacity is a read-only, explicitly requested quota inspection.
// No client-controlled destination path or transfer operation is supported.
func (h *Handler) remoteCapacity(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	names := query["remote"]
	if len(names) != 1 || !safeRemoteName.MatchString(names[0]) {
		writeErr(w, http.StatusBadRequest, "select exactly one configured Rclone remote")
		return
	}
	remote := names[0]
	select {
	case h.remoteGate <- struct{}{}:
		defer func() { <-h.remoteGate }()
	default:
		writeErr(w, http.StatusConflict, "another remote inspection is running")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Resolve the name against the connected local Rclone configuration,
	// rather than treating a URL parameter as an arbitrary provider path.
	listed, err := run(ctx, h.cfg.RcloneBin, "listremotes")
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "Rclone remote discovery is unavailable")
		return
	}
	found := false
	for _, line := range strings.Split(string(listed), "\n") {
		if strings.TrimSpace(line) == remote {
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, "remote is not configured")
		return
	}

	output, err := run(ctx, h.cfg.RcloneBin, "about", "--json", remote)
	if err != nil {
		writeErr(w, http.StatusNotImplemented, "this Rclone remote does not provide quota information")
		return
	}
	var values map[string]json.Number
	if err := json.Unmarshal(output, &values); err != nil || len(values) == 0 {
		writeErr(w, http.StatusBadGateway, "invalid Rclone quota response")
		return
	}
	quota := make(map[string]int64, 6)
	for _, key := range []string{"total", "used", "free", "trashed", "other", "objects"} {
		if value, ok := values[key]; ok {
			number, err := value.Int64()
			if err != nil || number < 0 {
				writeErr(w, http.StatusBadGateway, "invalid Rclone quota count")
				return
			}
			quota[key] = number
		}
	}
	if len(quota) == 0 {
		writeErr(w, http.StatusBadGateway, "Rclone did not return recognized quota fields")
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"remote": remote,
		"quota":  quota,
		"unit":   "bytes except objects",
		"note":   "Provider-reported quota may be unavailable or approximate",
	})
}
