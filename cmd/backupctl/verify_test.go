package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyRequiresExplicitCostConsent(t *testing.T) {
	t.Setenv("GCB_API_URL", defaultEndpoint)
	t.Setenv("GCB_API_TOKEN_FILE", "")
	var output, errors strings.Builder
	for _, args := range [][]string{
		{"verify"}, {"verify", "--accept-provider-costs=false"},
		{"verify", "--accept-provider-costs", "--timeout=1s"},
		{"verify", "--accept-provider-costs", "--source=Documents"},
	} {
		output.Reset()
		errors.Reset()
		if code := execute(args, &output, &errors); code != 2 {
			t.Errorf("%q bypassed consent/config validation: %d", args, code)
		}
	}
}

func TestVerifyArchiveMakesAuthenticatedReadOnlyRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/v1/verify" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Error("unexpected request method/path/auth")
		}
		if r.ContentLength > 0 {
			t.Error("verify request unexpectedly contained a body")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"verified":true,"filesRead":3,"bytesRead":12345}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := verifyArchive(ctx, server.URL, exampleToken)
	if err != nil {
		t.Fatal(err)
	}
	if !called || !result.Verified || result.FilesRead != 3 || result.BytesRead != 12345 {
		t.Fatalf("unexpected verify result: %+v", result)
	}
}

func TestVerifyRejectsRedirectErrorsAndMalformedBodies(t *testing.T) {
	cases := []struct {
		name     string
		code     int
		body     string
		redirect string
	}{
		{"healthyButNoVerified", 200, `{"filesRead":2}`, ""},
		{"zeroFiles", 200, `{"verified":true,"filesRead":0}`, ""},
		{"negativeBytes", 200, `{"verified":true,"filesRead":1,"bytesRead":-5}`, ""},
		{"invalidJSON", 200, `{`, ""},
		{"trailingJSON", 200, `{"verified":true,"filesRead":1} {}`, ""},
		{"giantBody", 200, `{"verified":true,"filesRead":1}` + strings.Repeat(" ", 5000), ""},
		{"serverError", 503, exampleToken, ""},
		{"redirect", 302, "", "https://untrusted.example"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if item.redirect != "" {
					w.Header().Set("Location", item.redirect)
				}
				w.WriteHeader(item.code)
				_, _ = w.Write([]byte(item.body))
			}))
			defer server.Close()
			_, err := verifyArchive(context.Background(), server.URL, exampleToken)
			if err == nil {
				t.Fatal("failed verification treated as healthy")
			}
			if strings.Contains(err.Error(), exampleToken) {
				t.Fatal("token leaked in verification error")
			}
		})
	}
}

func TestVerifyCLIWithPrivateTokenFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Error("missing token")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"verified":true,"filesRead":2,"bytesRead":100}`)
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(exampleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCB_API_URL", server.URL)
	t.Setenv("GCB_API_TOKEN_FILE", tokenPath)
	var output, errors strings.Builder
	if code := execute([]string{"verify", "--accept-provider-costs"}, &output, &errors); code != 0 {
		t.Fatalf("CLI rejected valid authorized verification: code=%d error=%s", code, errors.String())
	}
	if !strings.Contains(output.String(), "Verified 2") || strings.Contains(output.String(), exampleToken) {
		t.Fatal("output misleading or leaked token")
	}
}
