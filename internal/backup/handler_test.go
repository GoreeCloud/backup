package backup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

func testHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "safe"), 0700); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Config{Token: testToken, SourceRoot: root, KopiaBin: "/not/installed/kopia", RcloneBin: "/not/installed/rclone"})
	if err != nil {
		t.Fatal(err)
	}
	return handler, root
}
func TestAuthAndHealth(t *testing.T) {
	h, _ := testHandler(t)
	for _, tt := range []struct {
		path, auth string
		want       int
	}{
		{"/healthz", "", 200},
		{"/api/v1/status", "", 401},
		{"/api/v1/status", "Bearer " + testToken + "broken", 401},
		{"/api/v1/status", "Bearer " + testToken, 200},
	} {
		r := httptest.NewRequest(http.MethodGet, tt.path, nil)
		if tt.auth != "" {
			r.Header.Set("Authorization", tt.auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s: got %d want %d", tt.path, w.Code, tt.want)
		}
	}
}
func TestSourceEscapeBlocked(t *testing.T) {
	h, root := testHandler(t)
	_ = h
	handler := &Handler{root: root}
	for _, bad := range []string{"", "..", "../etc", "/etc", ".", "safe/../safe", "./safe"} {
		if _, err := handler.safeSource(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got, err := handler.safeSource("safe"); err != nil || got != filepath.Join(root, "safe") {
		t.Errorf("valid dir: %q %v", got, err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.safeSource("outside"); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
func TestMalformedBodyAndNoExecution(t *testing.T) {
	h, _ := testHandler(t)
	for _, body := range []string{`{"source":"../etc"}`, `{"source":"/etc"}`, `{"source":"safe","extra":true}`, `{"source":"safe"} {}`} {
		r := httptest.NewRequest("POST", "/api/v1/snapshots", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Errorf("body %q got %d: %s", body, w.Code, w.Body.String())
		}
	}
}
