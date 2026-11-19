package backup

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All paths are test-owned and disposable. No actual archive or user data is
// accessed; the non-existent engine makes accidental snapshots impossible.
func testAnchoredUploadRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/files", strings.NewReader("synthetic selected file"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-GCB-Filename-B64", base64.RawURLEncoding.EncodeToString([]byte("synthetic.txt")))
	req.Header.Set("X-GCB-Confirm-Upload", "true")
	req.Header.Set("Content-Type", "application/octet-stream")
	return req
}

func TestMobileIngestDirectoryReplacementRejected(t *testing.T) {
	source := t.TempDir()
	parent := t.TempDir()
	ingest := filepath.Join(parent, "ingest")
	if err := os.Mkdir(ingest, 0700); err != nil {
		t.Fatal(err)
	}
	api, err := NewHandler(Config{Token: testToken, SourceRoot: source, MobileIngestRoot: ingest, KopiaBin: "/invalid/kopia"})
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(parent, "old-ingest")
	if err := os.Rename(ingest, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ingest, 0700); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, testAnchoredUploadRequest())
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("replacement private directory was accepted: %d %s", response.Code, response.Body.String())
	}
	for _, location := range []string{ingest, old} {
		entries, err := os.ReadDir(location)
		if err != nil || len(entries) != 0 {
			t.Errorf("unexpected staging in %s: %v %v", location, entries, err)
		}
	}
}

func TestMobileIngestSymlinkReplacementRejected(t *testing.T) {
	source := t.TempDir()
	parent := t.TempDir()
	ingest := filepath.Join(parent, "ingest")
	victim := filepath.Join(parent, "victim")
	if err := os.Mkdir(ingest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victim, 0700); err != nil {
		t.Fatal(err)
	}
	api, err := NewHandler(Config{Token: testToken, SourceRoot: source, MobileIngestRoot: ingest, KopiaBin: "/invalid/kopia"})
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(parent, "old-ingest")
	if err := os.Rename(ingest, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, ingest); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, testAnchoredUploadRequest())
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("symlink-swapped ingest was accepted: %d", response.Code)
	}
	entries, err := os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatalf("victim received staging material: %v %v", entries, err)
	}
}

func TestRootedStagingWritesAndCleanupRemainInOriginalDirectory(t *testing.T) {
	parent := t.TempDir()
	ingest := filepath.Join(parent, "ingest")
	victim := filepath.Join(parent, "victim")
	if err := os.Mkdir(ingest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victim, 0700); err != nil {
		t.Fatal(err)
	}
	original, err := os.Lstat(ingest)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{mobileRoot: ingest, mobileInfo: original}
	root, err := h.openPrivateMobileRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	relocated := filepath.Join(parent, "relocated")
	if err := os.Rename(ingest, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, ingest); err != nil {
		t.Fatal(err)
	}

	if err := root.Mkdir("mobile-test", 0700); err != nil {
		t.Fatal(err)
	}
	child, err := root.OpenRoot("mobile-test")
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	out, err := child.OpenFile("document.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("disposable upload bytes")); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(relocated, "mobile-test", "document.txt")); err != nil {
		t.Fatalf("anchored write did not remain inside the original moved directory: %v", err)
	}
	entries, err := os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink destination received data: %v %v", entries, err)
	}
	if err := cleanupMobileWorkspace(root, child, "mobile-test", "document.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(relocated, "mobile-test")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("anchored cleanup did not remove old private workspace: %v", err)
	}
	entries, err = os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatalf("victim changed after cleanup: %v %v", entries, err)
	}
}

func TestAnchoredCleanupRejectsSubstitutedWorkspace(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{mobileRoot: parent, mobileInfo: originalInfo}
	root, err := h.openPrivateMobileRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("mobile-test", 0700); err != nil {
		t.Fatal(err)
	}
	child, err := root.OpenRoot("mobile-test")
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	out, err := child.OpenFile("file.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(parent, "mobile-test"), filepath.Join(parent, "relocated")); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("mobile-test", 0700); err != nil {
		t.Fatal(err)
	}
	if err := cleanupMobileWorkspace(root, child, "mobile-test", "file.txt"); err == nil {
		t.Fatal("cleanup accepted a substituted workspace directory")
	}
	if _, err := root.Lstat("mobile-test"); err != nil {
		t.Fatal("replacement directory unexpectedly removed:", err)
	}
}
