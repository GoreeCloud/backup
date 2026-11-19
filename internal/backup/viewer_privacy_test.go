package backup

import (
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

func TestMinimalViewerSnapshotsRemovesSensitiveMetadata(t *testing.T) {
	records := []map[string]any{
		{
			"id": "synpriv001",
			"source": map[string]any{
				"path": "/home/private-user/Backup Plans",
				"host": "private-hostname",
				"user": "private-user",
			},
			"startTime": "2026-11-19T00:00:00Z",
			"rootEntry": map[string]any{"obj": "secret-object-id"},
			"tags":      map[string]any{"personal": "sensitive tag"},
		},
		{
			"id": "synpriv002",
			"source": map[string]any{
				"path": `C:\Users\private-person\Personal Photos`,
				"host": "another-private-host",
				"user": "private-person",
			},
			"startTime": "2026-11-19T01:00:00Z",
		},
	}
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	output, err := minimalViewerSnapshots(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 2 {
		t.Fatalf("got %d view summaries", len(output))
	}
	if output[0].Source.Path != "Backup Plans" || output[1].Source.Path != "Personal Photos" {
		t.Fatalf("absolute source leaked or leaf names missing: %+v", output)
	}
	sanitized, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-user", "private-person", "private-hostname", "another-private-host",
		"secret-object-id", "sensitive tag", "C:\\Users", "/home/"} {
		if strings.Contains(string(sanitized), secret) {
			t.Errorf("viewer metadata leaked %q", secret)
		}
	}
	if output[0].ID != "synpriv001" || output[0].StartTime != "2026-11-19T00:00:00Z" {
		t.Fatal("viewer cannot use sanitized snapshot ID/timestamp")
	}
}

func TestViewerSnapshotMetadataFailsClosedOnMalformedResponse(t *testing.T) {
	for _, bad := range []string{"null", "{}", "false", "not-json"} {
		if _, err := minimalViewerSnapshots([]byte(bad)); err == nil {
			t.Errorf("accepted invalid list shape %q", bad)
		}
	}
	result, err := minimalViewerSnapshots([]byte(`[{"id":"synpriv003","source":{"path":"/"}},{"id":"unsafe/id","source":{"path":"/elsewhere"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Source.Path != "Protected snapshot" {
		t.Errorf("root path should have generic label, invalid ID should be skipped: %+v", result)
	}
}

func TestViewerAndAdminSnapshotAndStatusResponsesDiffer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX synthetic Kopia CLI")
	}
	work := t.TempDir()
	exe := filepath.Join(work, "synthetic-kopia")
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
  "repository status") printf 'Connected storage /home/private-account/secret-repository\\n';;
  "snapshot list") cat <<'JSON'
[{"id":"synpriv111","startTime":"2026-11-19T07:00:00Z","source":{"path":"/home/private-account/Invoices","user":"private-account","host":"private-host"},"rootEntry":{"obj":"private-content-object"}}]
JSON
    ;;
  *) exit 91;;
esac
`)
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	api, err := NewHandler(Config{
		Token:      testToken,
		ViewToken:  viewTestToken,
		SourceRoot: t.TempDir(),
		KopiaBin:   exe,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token  string
		viewer bool
	}{
		{viewTestToken, true}, {testToken, false},
	} {
		for _, route := range []string{"/api/v1/status", "/api/v1/snapshots"} {
			r := httptest.NewRequest(http.MethodGet, route, nil)
			r.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("%s viewer=%t got %d: %s", route, tc.viewer, w.Code, w.Body.String())
			}
			response := w.Body.String()
			if tc.viewer {
				for _, secret := range []string{"private-account", "private-host", "private-content-object", "/home/"} {
					if strings.Contains(response, secret) {
						t.Errorf("viewer leaked %s via %s", secret, route)
					}
				}
				if strings.HasSuffix(route, "/snapshots") && !strings.Contains(response, `"path":"Invoices"`) {
					t.Errorf("viewer listing lacks source leaf: %s", response)
				}
			} else if !strings.Contains(response, "private-account") {
				t.Errorf("administrator metadata was unexpectedly redacted: %s", response)
			}
		}
	}
}
