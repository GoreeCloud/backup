package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "testing-secret-token-that-is-at-least-24-bytes"

type fakeExecutor struct {
	mu    sync.Mutex
	count int
}

func (f *fakeExecutor) Run(ctx context.Context, binary string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	return "fake completed", nil
}
func TestAuthenticationAndHeaders(t *testing.T) {
	srv := NewServer(testToken, &fakeExecutor{})
	request := httptest.NewRequest("GET", "/api/v1/health", nil)
	result := httptest.NewRecorder()
	srv.ServeHTTP(result, request)
	if result.Code != 401 {
		t.Fatalf("without bearer got %d", result.Code)
	}
	if result.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
	request.Header.Set("Authorization", "Bearer wrong")
	result = httptest.NewRecorder()
	srv.ServeHTTP(result, request)
	if result.Code != 401 {
		t.Fatalf("wrong token got %d", result.Code)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	result = httptest.NewRecorder()
	srv.ServeHTTP(result, request)
	if result.Code != 200 {
		t.Fatalf("valid token got %d", result.Code)
	}
	request = httptest.NewRequest("GET", "/", nil)
	result = httptest.NewRecorder()
	srv.ServeHTTP(result, request)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "GoreeCloud Backup") {
		t.Fatal("dashboard unavailable")
	}
}
func TestJobsLifecycle(t *testing.T) {
	dir := t.TempDir()
	for _, bin := range []string{"kopia", "rclone"} {
		if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	fake := &fakeExecutor{}
	srv := NewServer(testToken, fake)
	payload, _ := json.Marshal(Request{Type: "backup", Source: t.TempDir()})
	req := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(string(payload)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	srv.ServeHTTP(response, req)
	if response.Code != 202 {
		t.Fatalf("job status %d: %s", response.Code, response.Body.String())
	}
	for i := 0; i < 40; i++ {
		list := httptest.NewRequest("GET", "/api/v1/jobs", nil)
		list.Header.Set("Authorization", "Bearer "+testToken)
		res := httptest.NewRecorder()
		srv.ServeHTTP(res, list)
		if strings.Contains(res.Body.String(), `"status":"completed"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not complete")
}
func TestDisallowedRequests(t *testing.T) {
	srv := NewServer(testToken, &fakeExecutor{})
	cases := []struct {
		body, contentType, origin string
		want                      int
	}{
		{`{"type":"sync","source":"remote:foo","destination":"remote:bar"}`, "application/json", "", 422},
		{`{"type":"copy","source":"-malicious","destination":"remote:bar"}`, "application/json", "", 422},
		{`{"type":"backup","source":"/tmp","unknown":true}`, "application/json", "", 400},
		{`{"type":"snapshots"}`, "text/plain", "", 415},
		{`{"type":"snapshots"}`, "application/json", "https://evil.example", 403},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", tc.contentType)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		response := httptest.NewRecorder()
		srv.ServeHTTP(response, req)
		if response.Code != tc.want {
			t.Fatalf("body %s: %d != %d: %s", tc.body, response.Code, tc.want, response.Body.String())
		}
	}
}

type gatedExecutor struct {
	release chan struct{}
}

func (g *gatedExecutor) Run(ctx context.Context, binary string, args ...string) (string, error) {
	select {
	case <-g.release:
		return "finished", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestJobAdmissionIsBounded(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "kopia"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	gated := &gatedExecutor{release: make(chan struct{})}
	defer close(gated.release)
	srv := NewServer(testToken, gated)
	send := func() int {
		t.Helper()
		req := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(`{"type":"snapshots"}`))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < maxPendingJobs; i++ {
		if code := send(); code != 202 {
			t.Fatalf("job %d returned %d", i, code)
		}
	}
	if code := send(); code != 429 {
		t.Fatalf("over-capacity job returned %d, want 429", code)
	}
}

func TestSyncRequiresFreshUnusedMatchingPreview(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "rclone"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	srv := NewServer(testToken, &fakeExecutor{})
	submit := func(kind, from, to, confirmation string) int {
		t.Helper()
		payload, err := json.Marshal(Request{Type: kind, Source: from, Destination: to, Confirm: confirmation})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(string(payload)))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}
	from, to := "remote:source", "remote:target"
	if got := submit("sync", from, to, "DELETE DESTINATION EXTRAS"); got != 409 {
		t.Fatalf("sync without a preview: got %d, want 409", got)
	}
	if got := submit("sync-preview", from, to, ""); got != 202 {
		t.Fatalf("preview submission: %d", got)
	}
	ready := false
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), `"status":"completed"`) {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("preview did not complete")
	}
	if got := submit("sync", from, "remote:other", "DELETE DESTINATION EXTRAS"); got != 409 {
		t.Fatalf("different destination accepted: %d", got)
	}
	if got := submit("sync", from, to, "DELETE DESTINATION EXTRAS"); got != 202 {
		t.Fatalf("confirmed previewed sync rejected: %d", got)
	}
	if got := submit("sync", from, to, "DELETE DESTINATION EXTRAS"); got != 409 {
		t.Fatalf("consumed preview reused: %d", got)
	}
}
