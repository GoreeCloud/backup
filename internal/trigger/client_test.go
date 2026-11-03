package trigger

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const exampleToken = "0123456789abcdef0123456789abcdef"
const exampleID = "abcdef012345abcdef012345"

func TestLoadTokenPrivateFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(f, []byte(exampleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken(f)
	if err != nil || got != exampleToken {
		t.Fatalf("read token: %q %v", got, err)
	}
	if err := os.Chmod(f, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken(f); err == nil {
		t.Fatal("accepted world-readable token")
	}
	if err := os.Chmod(f, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(f), "alias")
	if err := os.Symlink(f, alias); err == nil {
		if _, err := LoadToken(alias); err == nil {
			t.Fatal("accepted token file symlink")
		}
	}
}

func TestRejectInvalidTokenFileAndPaths(t *testing.T) {
	if _, err := LoadToken("relative"); err == nil {
		t.Fatal("accepted relative token filename")
	}
	f := filepath.Join(t.TempDir(), "short")
	if err := os.WriteFile(f, []byte("short\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken(f); err == nil {
		t.Fatal("accepted short token")
	}
	if err := os.WriteFile(f, []byte(strings.Repeat("x", 5000)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken(f); err == nil {
		t.Fatal("accepted overlarge token")
	}
}

func TestSubmitAndWaitSuccess(t *testing.T) {
	var polls atomic.Int32
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+exampleToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/jobs":
			posts.Add(1)
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("missing JSON content type")
			}
			w.WriteHeader(202)
			fmt.Fprintf(w, `{"id":"%s","status":"queued"}`, exampleID)
		case r.Method == "GET" && r.URL.Path == "/api/v1/jobs/"+exampleID:
			n := polls.Add(1)
			state := "running"
			if n >= 2 {
				state = "completed"
			}
			fmt.Fprintf(w, `{"id":"%s","status":"%s"}`, exampleID, state)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Token: exampleToken, PollInterval: time.Millisecond}
	id, err := c.SubmitAndWait(context.Background(), t.TempDir())
	if err != nil || id != exampleID {
		t.Fatalf("submit result id=%q err=%v", id, err)
	}
	if posts.Load() != 1 || polls.Load() < 2 {
		t.Fatal("unexpected request sequence")
	}
}

func TestFailureStatusAndDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(202)
			fmt.Fprintf(w, `{"id":"%s"}`, exampleID)
			return
		}
		fmt.Fprintf(w, `{"id":"%s","status":"failed"}`, exampleID)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Token: exampleToken, PollInterval: time.Millisecond}
	id, err := c.SubmitAndWait(context.Background(), t.TempDir())
	if id != exampleID || err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("wrong failure: id=%s err=%v", id, err)
	}
	c.BaseURL = "http://192.0.2.1:1234"
	if _, err := c.SubmitAndWait(context.Background(), t.TempDir()); err == nil {
		t.Fatal("accepted public destination")
	}
	if _, err := c.SubmitAndWait(context.Background(), "/"); err == nil {
		t.Fatal("accepted filesystem root backup")
	}
}

func TestPollingCanTimeOutWithoutClaimingSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(202)
			fmt.Fprintf(w, `{"id":"%s"}`, exampleID)
			return
		}
		fmt.Fprintf(w, `{"id":"%s","status":"running"}`, exampleID)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Token: exampleToken, PollInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	id, err := c.SubmitAndWait(ctx, t.TempDir())
	if id != exampleID || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected timeout: id=%q err=%v", id, err)
	}
}

func TestRedirectCannotReceiveBearerToken(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c := Client{BaseURL: source.URL, Token: exampleToken}
	if _, err := c.SubmitAndWait(context.Background(), t.TempDir()); err == nil {
		t.Fatal("followed or accepted redirected operation")
	}
	if redirected.Load() {
		t.Fatal("bearer token forwarded via redirect")
	}
}

func TestRejectsMalformedReturnedJobID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		fmt.Fprint(w, `{"id":"../bad"}`)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Token: exampleToken}
	if _, err := c.SubmitAndWait(context.Background(), t.TempDir()); err == nil {
		t.Fatal("accepted invalid API job ID")
	}
}
