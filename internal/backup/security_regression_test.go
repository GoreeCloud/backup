package backup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAmbiguousAuthorizationHeadersRejected(t *testing.T) {
	h, _ := testHandler(t)
	tests := []struct {
		headers []string
		status  int
	}{
		{[]string{"Bearer " + testToken}, http.StatusOK},
		{[]string{"Bearer " + testToken, "Bearer bad"}, http.StatusUnauthorized},
		{[]string{"Bearer " + testToken, "Bearer " + testToken}, http.StatusUnauthorized},
		{[]string{"Bearer " + testToken + ", Bearer bad"}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		for _, v := range tc.headers {
			r.Header.Add("Authorization", v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%v: got %d want %d", len(tc.headers), w.Code, tc.status)
		}
	}
}

func TestJSONContentTypeRejectsAmbiguousInputs(t *testing.T) {
	h, _ := testHandler(t)
	for _, headers := range [][]string{nil, {"text/plain"}, {"application/json", "application/json"}, {"application/json,application/json"}} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", strings.NewReader(`{"source":"safe"}`))
		r.Header.Set("Authorization", "Bearer "+testToken)
		for _, value := range headers {
			r.Header.Add("Content-Type", value)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("headers %q: %d", headers, w.Code)
		}
	}
}

func TestWebServerBlocksOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("HOME"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("sensitive data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(web, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{SourceRoot: root, Token: testToken, WebDir: web})
	if err != nil {
		t.Fatal(err)
	}
	bad := httptest.NewRequest("GET", "/leak.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, bad)
	if w.Code != http.StatusForbidden {
		t.Fatalf("web symlink leak code %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "sensitive data") {
		t.Fatal("outside file content leaked")
	}
	good := httptest.NewRequest("GET", "/", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, good)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "HOME") {
		t.Fatal("index not served")
	}
}

func TestRecoverySymlinkEscapesRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data.txt", filepath.Join(root, "safe-link")); err != nil {
		t.Fatal(err)
	}
	if err := auditRecoveredTree(root); err != nil {
		t.Fatalf("internal relative link rejected: %v", err)
	}
	if err := os.Symlink("../outside", filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := auditRecoveredTree(root); err == nil {
		t.Fatal("traversal symlink accepted")
	}
	if err := os.Remove(filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "absolute")); err != nil {
		t.Fatal(err)
	}
	if err := auditRecoveredTree(root); err == nil {
		t.Fatal("absolute link accepted")
	}
}

func TestRecoveryDanglingSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := auditRecoveredTree(root); err == nil {
		t.Fatal("dangling symlink accepted")
	}
}
