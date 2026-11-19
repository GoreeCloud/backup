package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doctorInfoFixture(readOnly, canSnapshot, canVerify, restore bool) map[string]any {
	return map[string]any{
		"name": "GoreeCloud Backup", "readOnly": readOnly,
		"canSnapshot": canSnapshot, "canVerify": canVerify, "restoreEnabled": restore,
	}
}

func doctorTestServer(t *testing.T, connected, hasSnapshots bool, readOnly bool, visited *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*visited = append(*visited, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet {
			t.Errorf("mutating request: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Error("wrong bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/info":
			_ = json.NewEncoder(w).Encode(doctorInfoFixture(readOnly, !readOnly, !readOnly, false))
		case "/api/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"connected": connected})
		case "/api/v1/snapshots":
			if !connected {
				t.Error("queried snapshots while disconnected")
			}
			var snapshots []map[string]any
			if hasSnapshots {
				snapshots = []map[string]any{{"id": "doctorsnap001", "startTime": "2026-11-19T00:00:00Z",
					"source": map[string]any{"path": "Home"}}}
			} else {
				snapshots = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(snapshots)
		default:
			t.Errorf("unexpected endpoint %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
}

func setupDoctorToken(t *testing.T, endpoint string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "viewer-token")
	if err := os.WriteFile(file, []byte(exampleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCB_API_URL", endpoint)
	t.Setenv("GCB_API_TOKEN_FILE", file)
}

func TestDoctorUsesOnlyViewerMetadataGET(t *testing.T) {
	var visited []string
	server := doctorTestServer(t, true, true, true, &visited)
	defer server.Close()
	setupDoctorToken(t, server.URL)
	var out, errs strings.Builder
	if code := execute([]string{"doctor"}, &out, &errs); code != 0 {
		t.Fatalf("doctor did not pass metadata-only preflight: %d %s", code, errs.String())
	}
	for _, needle := range []string{
		"Credential scope: read-only viewer", "Repository connection: connected",
		"at least one visible snapshot", "Snapshot writes permitted by token: false",
		"Contained restore permitted and configured for token: false",
		"No archived file contents were verified or restored",
	} {
		if !strings.Contains(out.String(), needle) {
			t.Errorf("missing %q: %s", needle, out.String())
		}
	}
	if strings.Contains(out.String(), exampleToken) || strings.Contains(errs.String(), exampleToken) {
		t.Fatal("preflight exposed credential")
	}
	if strings.Join(visited, ",") != "GET /api/v1/info,GET /api/v1/status,GET /api/v1/snapshots" {
		t.Errorf("unexpected operations: %+v", visited)
	}
}

func TestDoctorFailsWhenDisconnectedOrNoVisibleRecoveryPoints(t *testing.T) {
	for _, tc := range []struct {
		name      string
		connected bool
		present   bool
	}{
		{"offline", false, true}, {"no-points", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var visited []string
			server := doctorTestServer(t, tc.connected, tc.present, false, &visited)
			defer server.Close()
			setupDoctorToken(t, server.URL)
			var out, errs strings.Builder
			if code := execute([]string{"doctor"}, &out, &errs); code != 1 {
				t.Errorf("accepted unavailable recovery preflight: %d", code)
			}
			if !strings.Contains(errs.String(), "Preflight NOT ready") {
				t.Errorf("expected not-ready warning: %q", errs.String())
			}
			if !tc.connected && len(visited) != 2 {
				t.Errorf("unexpected offline queries: %v", visited)
			}
		})
	}
}

func TestDoctorRejectsForgedCapabilities(t *testing.T) {
	fixtures := []map[string]any{
		{}, {"name": "Other product", "readOnly": true, "canSnapshot": false, "canVerify": false, "restoreEnabled": false},
		doctorInfoFixture(true, true, false, false),
		{"name": "GoreeCloud Backup", "readOnly": true, "canSnapshot": false, "canVerify": false},
	}
	for _, fixture := range fixtures {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(fixture)
		}))
		_, err := doctorMetadata(context.Background(), server.URL, exampleToken)
		server.Close()
		if err == nil {
			t.Errorf("accepted missing/privileged viewer metadata: %+v", fixture)
		}
	}
}

func TestDoctorArgumentsAndEndpointFailClosed(t *testing.T) {
	var out, errs strings.Builder
	for _, args := range [][]string{{"doctor", "--timeout=1s"}, {"doctor", "--timeout=3m"}, {"doctor", "unexpected"}} {
		out.Reset()
		errs.Reset()
		if code := execute(args, &out, &errs); code != 2 {
			t.Errorf("accepted invalid arguments %v", args)
		}
	}
	t.Setenv("GCB_API_URL", "http://example.org:8787")
	t.Setenv("GCB_API_TOKEN_FILE", "/does-not-exist")
	out.Reset()
	errs.Reset()
	if code := execute([]string{"doctor"}, &out, &errs); code != 2 {
		t.Error("allowed external API endpoint")
	}
	if strings.Contains(errs.String(), exampleToken) {
		t.Fatal("unexpected credential in error")
	}
}

func TestDoctorNeverPrintsUntrustedErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "private server diagnostic must not escape")
	}))
	defer server.Close()
	var result doctorInfo
	err := doctorGET(context.Background(), server.URL, exampleToken, "/api/v1/info", &result)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") ||
		strings.Contains(err.Error(), "private server diagnostic") {
		t.Fatalf("unsafe HTTP diagnostic: %v", err)
	}
}

func TestDoctorRejectsOversizedOrTrailingJSON(t *testing.T) {
	for _, payload := range []string{
		strings.Repeat(" ", 4097),
		"{\"name\":\"GoreeCloud Backup\"}{\"private\":\"secret\"}",
		"{\"name\":\"GoreeCloud Backup\"}garbage",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, payload)
		}))
		var result doctorInfo
		err := doctorGET(context.Background(), server.URL, exampleToken, "/api/v1/info", &result)
		server.Close()
		if err == nil {
			t.Error("accepted oversized or invalid metadata response")
		}
	}
}
