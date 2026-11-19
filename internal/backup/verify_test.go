package backup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBearerTokenNotInheritedByChild(t *testing.T) {
	// Execute a test-owned fixed script to check environment isolation.
	t.Setenv("GCB_API_TOKEN", testToken)
	exe := filepath.Join(t.TempDir(), "inspect.sh")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '%s' \"${GCB_API_TOKEN-unset}\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := run(ctx, exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "unset" {
		t.Fatalf("upstream executable received API token or unexpected env: %q", output)
	}
}

func TestVerifyAuthorizationAndConcurrency(t *testing.T) {
	h, root := testHandler(t)
	// The token gate is checked before any CLI call.
	unauth := httptest.NewRequest("POST", "/api/v1/verify", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, unauth)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated verify: %d", w.Code)
	}

	// Ensure a second repository operation is rejected even when the CLI is absent.
	handler := &Handler{root: root, jobGate: make(chan struct{}, 1)}
	handler.jobGate <- struct{}{}
	r := httptest.NewRequest("POST", "/api/v1/verify", nil)
	w = httptest.NewRecorder()
	handler.verify(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("overlapping verify: %d", w.Code)
	}

	r = httptest.NewRequest("POST", "/api/v1/snapshots", strings.NewReader(`{"source":"safe"}`))
	w = httptest.NewRecorder()
	handler.create(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("overlapping snapshot: %d", w.Code)
	}
	<-handler.jobGate
}
