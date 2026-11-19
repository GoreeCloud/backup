package backup

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMobileIngestRootConstraints(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "mobile")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := validateMobileRoot(root, private); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{root, t.TempDir(), "../mobile", "/not/existing"} {
		if _, err := validateMobileRoot(root, unsafe); err == nil {
			t.Errorf("allowed ingest root %s", unsafe)
		}
	}
	if err := os.Chmod(private, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := validateMobileRoot(root, private); err == nil {
		t.Fatal("allowed public-readable inbox")
	}
}

func TestFilenameMustBeLeafAndSafe(t *testing.T) {
	for _, good := range []string{"photo.jpg", "会議ノート.pdf", "My notes (2).txt"} {
		if !safeMobileFilename(good) {
			t.Errorf("rejected safe leaf: %q", good)
		}
	}
	for _, bad := range []string{"", "..", ".", "../secret", "/etc/passwd", "folder\\file.txt", "x\nToken", " trailing ", "file\x00name", strings.Repeat("b", 161)} {
		if safeMobileFilename(bad) {
			t.Errorf("allowed unsafe leaf %q", bad)
		}
	}
}

func TestEncodedMobileFilenameValidation(t *testing.T) {
	original := "会議ノート.pdf"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(original))
	bytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || !safeMobileFilename(string(bytes)) {
		t.Fatalf("unicode filename not preserved: %v", err)
	}
	for _, bad := range []string{"!!!!", "a/b", "not%20base64"} {
		if _, err := base64.RawURLEncoding.DecodeString(bad); err == nil {
			t.Errorf("accepted malformed base64url %s", bad)
		}
	}
}

func TestMobileUploadRequiresPrivateRootAndAuth(t *testing.T) {
	h, _ := testHandler(t)
	r := httptest.NewRequest("POST", "/api/v1/mobile/files", strings.NewReader("test"))
	r.Header.Set("X-GCB-Filename-B64", base64.RawURLEncoding.EncodeToString([]byte("phone.jpg")))
	r.Header.Set("X-GCB-Confirm-Upload", "true")
	r.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer accepted: %d", w.Code)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("disabled mobile ingest accepted: %d", w.Code)
	}
}

func TestMobileUploadInvalidRequestsDoNotStage(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "mobile")
	if err := os.Mkdir(inbox, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{Token: testToken, SourceRoot: root, MobileIngestRoot: inbox, KopiaBin: "/not/installed/kopia"})
	if err != nil {
		t.Fatal(err)
	}
	variants := []struct {
		filename, confirmation, media string
		size                          int64
	}{
		{"../escape", "true", "application/octet-stream", 8},
		{"notes.txt", "false", "application/octet-stream", 8},
		{"notes.txt", "true", "text/plain", 8},
		{"notes.txt", "true", "application/octet-stream", maxMobileFileBytes + 1},
	}
	for _, test := range variants {
		r := httptest.NewRequest("POST", "/api/v1/mobile/files", strings.NewReader("test"))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("X-GCB-Filename-B64", base64.RawURLEncoding.EncodeToString([]byte(test.filename)))
		r.Header.Set("X-GCB-Confirm-Upload", test.confirmation)
		r.Header.Set("Content-Type", test.media)
		r.ContentLength = test.size
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Errorf("invalid request accepted: %+v", test)
		}
	}
	entries, err := os.ReadDir(inbox)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staged files from bad input: %v %v", entries, err)
	}
}

func TestMobileUploadFailedSnapshotCleansPlaintext(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "private")
	if err := os.Mkdir(inbox, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{Token: testToken, SourceRoot: root, MobileIngestRoot: inbox, KopiaBin: "/not/installed/kopia"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/mobile/files", strings.NewReader("synthetic secret bytes"))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("X-GCB-Filename-B64", base64.RawURLEncoding.EncodeToString([]byte("secret.txt")))
	r.Header.Set("X-GCB-Confirm-Upload", "true")
	r.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("fake CLI expected to fail: %d %s", w.Code, w.Body.String())
	}
	entries, err := os.ReadDir(inbox)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staged plaintext not removed: %v %v", entries, err)
	}
}

func TestMobileUploadReturnsVerifiedByteCountAndSHA256(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("synthetic successful CLI uses /bin/true")
	}
	root := t.TempDir()
	inbox := filepath.Join(root, "mobile")
	if err := os.Mkdir(inbox, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{Token: testToken, SourceRoot: root, MobileIngestRoot: inbox, KopiaBin: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	source := "GoreeCloud synthetic file-content checksum evidence\n"
	r := httptest.NewRequest("POST", "/api/v1/mobile/files", strings.NewReader(source))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("X-GCB-Filename-B64", base64.RawURLEncoding.EncodeToString([]byte("photo.txt")))
	r.Header.Set("X-GCB-Confirm-Upload", "true")
	r.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("snapshot response: %d %s", w.Code, w.Body.String())
	}
	var outcome struct {
		BackedUp bool   `json:"backedUp"`
		Name     string `json:"name"`
		Bytes    int64  `json:"bytes"`
		SHA256   string `json:"sha256"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(source))
	if !outcome.BackedUp || outcome.Name != "photo.txt" || outcome.Bytes != int64(len(source)) || outcome.SHA256 != fmt.Sprintf("%x", expected) {
		t.Fatalf("unexpected upload integrity evidence: %+v", outcome)
	}
	entries, err := os.ReadDir(inbox)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staged plaintext not removed: %v %v", entries, err)
	}
}
