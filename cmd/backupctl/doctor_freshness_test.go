package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoctorFreshnessIsOptInAndChecksNewestTimestamp(t *testing.T) {
	now := time.Now().UTC()
	older := now.Add(-72 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-3 * time.Hour).Format(time.RFC3339)
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Errorf("token was not sent privately")
		}
		if r.Method != http.MethodGet {
			t.Errorf("freshness check attempted a mutation")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/info":
			_ = json.NewEncoder(w).Encode(doctorInfoFixture(true, false, false, false))
		case "/api/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"connected": true})
		case "/api/v1/snapshots":
			// Deliberately not ordered by timestamp; a mere list-last check
			// could incorrectly treat this as a 72-hour-old repository.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "snapshot0001", "startTime": older, "source": map[string]any{"path": "/private/old"}},
				{"id": "snapshot0002", "startTime": recent, "source": map[string]any{"path": "/private/new"}},
				{"id": "snapshot0003", "startTime": older, "source": map[string]any{"path": "/private/old"}},
			})
		default:
			t.Errorf("unexpected API path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(exampleToken), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCB_API_URL", server.URL)
	t.Setenv("GCB_API_TOKEN_FILE", tokenPath)

	for _, tc := range []struct {
		arguments []string
		expected  int
		output    string
	}{
		{[]string{"doctor"}, 0, "Newest visible snapshot timestamp (UTC):"},
		{[]string{"doctor", "--max-age=6h"}, 0, "Freshness threshold met"},
		{[]string{"doctor", "--max-age=1h"}, 1, "Preflight NOT fresh"},
		{[]string{"doctor", "--max-age=0s"}, 0, "No archived file contents were verified"},
	} {
		var out, err strings.Builder
		code := execute(tc.arguments, &out, &err)
		if code != tc.expected {
			t.Errorf("arguments %v: code %d expected %d; stdout=%s stderr=%s",
				tc.arguments, code, tc.expected, out.String(), err.String())
		}
		if !strings.Contains(out.String()+err.String(), tc.output) {
			t.Errorf("arguments %v missing expected output %q", tc.arguments, tc.output)
		}
		if strings.Contains(out.String(), exampleToken) || strings.Contains(err.String(), exampleToken) {
			t.Error("preflight printed a bearer credential")
		}
		if strings.Contains(out.String(), "/private/") {
			t.Error("preflight leaked a source path")
		}
	}
	for _, req := range seen {
		if !strings.HasPrefix(req, "GET ") {
			t.Errorf("mutating operation in preflight: %s", req)
		}
	}
}

func TestDoctorFreshnessFailsClosedForUnknownFutureOrStaleTime(t *testing.T) {
	now := time.Date(2026, 11, 19, 18, 0, 0, 0, time.UTC)
	tc := []struct {
		name      string
		newest    time.Time
		threshold time.Duration
		errorPart string
	}{
		{"unknown", time.Time{}, 24 * time.Hour, "no valid dated"},
		{"stale", now.Add(-48 * time.Hour), 24 * time.Hour, "older than"},
		{"future", now.Add(6 * time.Minute), 24 * time.Hour, "future"},
		{"bad-threshold", now, 0, "positive"},
	}
	for _, item := range tc {
		t.Run(item.name, func(t *testing.T) {
			err := checkSnapshotFreshness(item.newest, now, item.threshold)
			if err == nil || !strings.Contains(err.Error(), item.errorPart) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if err := checkSnapshotFreshness(now.Add(-time.Hour), now, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := checkSnapshotFreshness(now.Add(time.Minute), now, time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorFreshnessRejectsInvalidDurationBeforeNetwork(t *testing.T) {
	for _, arg := range []string{"--max-age=-1s", "--max-age=1s", "--max-age=8761h", "--max-age=invalid"} {
		var out, errors strings.Builder
		code := execute([]string{"doctor", arg}, &out, &errors)
		if code != 2 {
			t.Errorf("accepted %s: %d", arg, code)
		}
	}
}
