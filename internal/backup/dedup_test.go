package backup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFullVerificationAcceptsDeduplicatedSnapshotReferences(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX shell")
	}
	root := t.TempDir()
	script := filepath.Join(t.TempDir(), "kopia-fake")
	report := `{"stats":{"processedObjectCount":3,"expectedTotalObjectCount":6,"readFileCount":2,"expectedTotalFileCount":4,"readBytes":72},"errorCount":0}`
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' '"+report+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Config{
		SourceRoot: root, Token: testToken, KopiaBin: script, RcloneBin: "/not/used",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/verify", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("deduplicated report falsely rejected: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Verified       bool  `json:"verified"`
		FilesRead      int64 `json:"filesRead"`
		FileReferences int64 `json:"fileReferences"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Verified || payload.FilesRead != 2 || payload.FileReferences != 4 {
		t.Fatalf("bad dedup interpretation: %+v", payload)
	}
}

func TestFullVerificationRejectsEmptyArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX shell")
	}
	root := t.TempDir()
	script := filepath.Join(t.TempDir(), "kopia-fake")
	report := `{"stats":{"processedObjectCount":0,"expectedTotalObjectCount":0,"readFileCount":0,"expectedTotalFileCount":0,"readBytes":0},"errorCount":0}`
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' '"+report+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Config{SourceRoot: root, Token: testToken, KopiaBin: script, RcloneBin: "/not/used"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/verify", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("empty archive should not be certified: %d", response.Code)
	}
}
