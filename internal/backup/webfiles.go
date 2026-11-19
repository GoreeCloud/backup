package backup

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// serveWeb serves only regular files physically contained in the configured
// resolved web directory. An internal SPA fallback is permitted; symlink
// escapes and directory listings are not.
func (h *Handler) serveWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	relative := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
	requested := filepath.Join(h.webDir, relative)
	found, err := os.Lstat(requested)
	if err == nil && found.Mode().IsRegular() || err == nil && found.Mode()&os.ModeSymlink != 0 {
		real, resErr := filepath.EvalSymlinks(requested)
		if resErr != nil || !pathInside(h.webDir, real) {
			http.Error(w, "invalid web asset", http.StatusForbidden)
			return
		}
		stat, statErr := os.Stat(real)
		if statErr != nil || !stat.Mode().IsRegular() {
			http.Error(w, "invalid web asset", http.StatusForbidden)
			return
		}
		http.ServeFile(w, r, real)
		return
	}
	// An unknown path (including a directory) is a SPA route, never a
	// filesystem directory listing or arbitrary file.
	index, resErr := filepath.EvalSymlinks(filepath.Join(h.webDir, "index.html"))
	if resErr != nil || !pathInside(h.webDir, index) {
		http.Error(w, "invalid index file", http.StatusServiceUnavailable)
		return
	}
	stat, statErr := os.Stat(index)
	if statErr != nil || !stat.Mode().IsRegular() {
		http.Error(w, "invalid index file", http.StatusServiceUnavailable)
		return
	}
	http.ServeFile(w, r, index)
}
