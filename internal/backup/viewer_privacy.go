package backup

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"unicode"
)

// isViewerRequest reads only the role established by the trusted authorization
// middleware. Never derive privileges from a caller-supplied query or header.
func isViewerRequest(r *http.Request) bool {
	viewer, ok := r.Context().Value(authRoleKey{}).(bool)
	return ok && viewer
}

type viewSnapshot struct {
	ID     string `json:"id"`
	Source struct {
		Path string `json:"path"`
	} `json:"source"`
	StartTime string `json:"startTime,omitempty"`
}

// minimalViewerSnapshots copies only explicitly permitted display fields.
// Kopia's original snapshot JSON can include absolute source paths, machine
// hostnames, OS usernames, object IDs and user-provided tags; none are returned.
func minimalViewerSnapshots(input []byte) ([]viewSnapshot, error) {
	var sources []struct {
		ID     string `json:"id"`
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
		StartTime string `json:"startTime"`
	}
	if err := json.Unmarshal(input, &sources); err != nil || sources == nil {
		return nil, errors.New("invalid Kopia snapshot list format")
	}
	result := make([]viewSnapshot, 0, len(sources))
	for _, entry := range sources {
		if !snapshotIDFormat.MatchString(entry.ID) {
			continue
		}
		normalized := strings.ReplaceAll(entry.Source.Path, "\\", "/")
		name := path.Base(strings.TrimRight(normalized, "/"))
		if name == "" || name == "." || strings.HasSuffix(name, ":") {
			name = "Protected snapshot"
		}
		name = strings.Map(func(value rune) rune {
			if unicode.IsControl(value) {
				return -1
			}
			return value
		}, name)
		if name == "" {
			name = "Protected snapshot"
		}
		chars := []rune(name)
		if len(chars) > 80 {
			name = string(chars[:80])
		}
		var item viewSnapshot
		item.ID = entry.ID
		item.Source.Path = name
		item.StartTime = entry.StartTime
		result = append(result, item)
	}
	return result, nil
}
