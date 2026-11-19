package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const exampleToken = "0123456789abcdef0123456789abcdef"

func TestValidateSource(t *testing.T) {
	for _, source := range []string{"Documents", "folder/subfolder", "a-1_b"} {
		if err := validateSource(source); err != nil {
			t.Errorf("rejected %q: %v", source, err)
		}
	}
	for _, source := range []string{"", "../etc", "/etc", "a/../b", "a//b", "./a", "x\\y", "foo\nbar"} {
		if err := validateSource(source); err == nil {
			t.Errorf("accepted unsafe source %q", source)
		}
	}
}
func TestValidateEndpoint(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:8787", "http://127.0.0.1:12345"} {
		if err := validateEndpoint(ok); err != nil {
			t.Errorf("rejected %s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "http://localhost:8787", "http://127.0.0.2:8787", "https://example.com:8787", "http://127.0.0.1.evil.com:8787", "http://user@127.0.0.1:8787", "http://127.0.0.1:8787/evil", "http://127.0.0.1:8787?q=1", "http://127.0.0.1:0", "http://127.0.0.1", "http://127.0.0.1:65536"} {
		if err := validateEndpoint(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
func TestPrivateTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access-token")
	if err := os.WriteFile(path, []byte(exampleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readTokenFile(path)
	if err != nil || got != exampleToken {
		t.Fatalf("token file: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(path); err == nil {
		t.Fatal("accepted world readable token")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(link); err == nil {
		t.Fatal("accepted symlink token file")
	}
	if _, err := readTokenFile(""); err == nil {
		t.Fatal("accepted empty token file")
	}
}
func TestLocalSnapshotRequest(t *testing.T) {
	expectedSource := "Documents"
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "POST" || r.URL.Path != "/api/v1/snapshots" {
			t.Error("invalid method or path")
		}
		if r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Error("missing bearer token")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["source"] != expectedSource {
			t.Error("invalid source")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer server.Close()
	if err := validateEndpoint(server.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := snapshot(ctx, server.URL, expectedSource, exampleToken); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("server was not called")
	}
}
func TestServerFailureDoesNotLeakSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, exampleToken, 500)
	}))
	defer server.Close()
	err := snapshot(context.Background(), server.URL, "Documents", exampleToken)
	if err == nil || strings.Contains(err.Error(), exampleToken) {
		t.Fatalf("secret leakage or missing error")
	}
}
func TestExecuteRefusesMissingTokenAndUnsafeURL(t *testing.T) {
	// No network request is made for missing/misconfigured credential files.
	t.Setenv("GCB_API_URL", defaultEndpoint)
	t.Setenv("GCB_API_TOKEN_FILE", "")
	var out, errors strings.Builder
	if code := execute([]string{"snapshot", "--source=Documents"}, &out, &errors); code != 2 {
		t.Fatalf("expected config error, got %d", code)
	}
	t.Setenv("GCB_API_URL", "https://example.com")
	if code := execute([]string{"snapshot", "--source=Documents"}, &out, &errors); code != 2 {
		t.Fatalf("invalid endpoint accepted, got %d", code)
	}
}

func TestNoRedirectAndNoTokenLeakage(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		if r.Header.Get("Authorization") != "" {
			t.Error("bearer token forwarded to redirect")
		}
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	err := snapshot(context.Background(), redirect.URL, "Documents", exampleToken)
	if err == nil {
		t.Fatal("redirect treated as a snapshot success")
	}
	if forwarded {
		t.Fatal("redirect followed; token could have leaked")
	}
}
