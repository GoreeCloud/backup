package backup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreRootDisjointAndPermissions(t *testing.T) {
	source := t.TempDir()
	inside := filepath.Join(source, "inside")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRestoreRoot(source, inside); err == nil {
		t.Fatal("accepted overlapping source and restore")
	}
	if _, err := validateRestoreRoot(source, source); err == nil {
		t.Fatal("accepted identical roots")
	}
	if _, err := validateRestoreRoot(source, filepath.Dir(source)); err == nil {
		t.Fatal("accepted restore ancestor")
	}
	if _, err := validateRestoreRoot(source, "../relative"); err == nil {
		t.Fatal("accepted relative restore root")
	}
	outside := t.TempDir()
	if err := os.Chmod(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := validateRestoreRoot(source, outside); err != nil || got != outside {
		t.Fatalf("valid restore root: %s %v", got, err)
	}
	if err := os.Chmod(outside, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRestoreRoot(source, outside); err == nil {
		t.Fatal("accepted world-writable restore root")
	}
}

func TestRestoreInputNoSideEffects(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "Docs"), 0700); err != nil {
		t.Fatal(err)
	}
	restoreRoot := t.TempDir()
	if err := os.Chmod(restoreRoot, 0700); err != nil {
		t.Fatal(err)
	}
	api, err := NewHandler(Config{Token: testToken, SourceRoot: source, RestoreRoot: restoreRoot, KopiaBin: "/not-installed/kopia", RcloneBin: "/not-installed/rclone"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"snapshotId":"../escape","acknowledge":true}`,
		`{"snapshotId":"deadbeef","acknowledge":false}`,
		`{"snapshotId":"deadbeef","acknowledge":true,"path":"/tmp"}`,
		`{"snapshotId":"deadbeef","acknowledge":true} {}`,
	} {
		r := httptest.NewRequest("POST", "/api/v1/restore", strings.NewReader(input))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q response %d: %s", input, w.Code, w.Body.String())
		}
	}
	entries, err := os.ReadDir(restoreRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid payload created %d restore directories", len(entries))
	}
	r := httptest.NewRequest("POST", "/api/v1/restore", strings.NewReader(`{"snapshotId":"deadbeef","acknowledge":true}`))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing Kopia got %d: %s", w.Code, w.Body.String())
	}
	entries, err = os.ReadDir(restoreRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("missing Kopia wrote restore output")
	}
}

func TestRestoreDisabledAndAuthorization(t *testing.T) {
	h, _ := testHandler(t)
	r := httptest.NewRequest("POST", "/api/v1/restore", strings.NewReader(`{"snapshotId":"deadbeef","acknowledge":true}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatal(w.Code)
	}
}
