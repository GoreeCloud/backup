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
)

func TestSnapshotListSanitizesPathsAndHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/snapshots" || r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Error("unexpected method/path/bearer")
		}
		fmt.Fprint(w, `[
          {"id":"snap00101","startTime":"2026-11-19T16:30:00Z",
           "source":{"path":"/home/private-username/Documents","host":"private-host","user":"private-username"},
           "rootEntry":{"obj":"private-root-object"},"tags":{"secret":"private-tag"}},
          {"id":"snap00102","startTime":"2026-11-19T16:31:00Z",
           "source":{"path":"C:\\Users\\other-person\\Personal Photos","host":"other-host"},
           "rootEntry":{"obj":"private-object-2"}},
          {"id":"unsafe/id","startTime":"2026-11-19T00:00:00Z","source":{"path":"/private/invalid"}}
        ]`)
	}))
	defer server.Close()
	summaries, err := listSnapshots(context.Background(), server.URL, exampleToken, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d valid snapshots", len(summaries))
	}
	if summaries[0].ID != "snap00102" || summaries[0].Label != "Personal Photos" ||
		summaries[1].ID != "snap00101" || summaries[1].Label != "Documents" {
		t.Fatalf("unexpected sanitized listing %+v", summaries)
	}
	if summaries[0].Timestamp != "2026-11-19T16:31:00Z" {
		t.Fatalf("invalid time: %q", summaries[0].Timestamp)
	}
	private := fmt.Sprintf("%+v", summaries)
	for _, value := range []string{"private-username", "private-host", "private-root-object", "private-tag", "C:\\Users", "/home/"} {
		if strings.Contains(private, value) {
			t.Errorf("CLI exposed sensitive metadata %q", value)
		}
	}
	latest, err := listSnapshots(context.Background(), server.URL, exampleToken, 1)
	if err != nil || len(latest) != 1 || latest[0].ID != "snap00102" {
		t.Fatalf("limit not enforced: %+v %v", latest, err)
	}
}

func TestSnapshotListRejectsBadResponseOrRedirect(t *testing.T) {
	cases := []struct {
		name string
		code int
		body string
	}{
		{"not-array", 200, `{}`},
		{"null", 200, "null"},
		{"truncated", 200, `[{"id":"good0001"}`},
		{"trailing", 200, `[]{}`},
		{"oversized", 200, "[]" + strings.Repeat(" ", 2<<20)},
		{"unavailable", 503, "private token should not appear"},
		{"redirect", 302, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				if tc.code == 302 {
					w.Header().Set("Location", "https://untrusted.example")
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := listSnapshots(context.Background(), server.URL, exampleToken, 50)
			if err == nil {
				t.Fatal("malformed/untrusted response accepted")
			}
			if requestCount != 1 {
				t.Fatal("unexpected HTTP redirect or second request")
			}
			if strings.Contains(err.Error(), exampleToken) {
				t.Fatal("bearer token leaked")
			}
		})
	}
}

func TestSnapshotListCLIWithViewerPrivateTokenFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Error("viewer credential absent")
		}
		fmt.Fprint(w, `[{"id":"viewtest01","startTime":"2026-11-19T12:00:00-04:00","source":{"path":"Sensitive"}}]`)
	}))
	defer server.Close()
	keyPath := filepath.Join(t.TempDir(), "viewer-token")
	if err := os.WriteFile(keyPath, []byte(exampleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCB_API_URL", server.URL)
	t.Setenv("GCB_API_TOKEN_FILE", keyPath)
	var out, errs strings.Builder
	if code := execute([]string{"list", "--limit=5", "--timeout=15s"}, &out, &errs); code != 0 {
		t.Fatalf("list failed %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "viewtest01") || !strings.Contains(out.String(), "2026-11-19T16:00:00Z") ||
		strings.Contains(out.String(), exampleToken) {
		t.Fatalf("bad list output %q", out.String())
	}
	for _, args := range [][]string{{"list", "--limit=0"}, {"list", "--limit=101"}, {"list", "--timeout=1s"}, {"list", "extra"}} {
		out.Reset()
		errs.Reset()
		if code := execute(args, &out, &errs); code != 2 {
			t.Errorf("accepted invalid list flags %v", args)
		}
	}
}

func TestSnapshotDisplayControlSanitization(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{"/", "Protected snapshot"},
		{"C:\\", "Protected snapshot"},
		{"/home/user/Leaf\nName", "LeafName"},
		{"/home/user/Documents/", "Documents"},
		{"a/", "a"},
	} {
		if got := safeSnapshotDisplay(tc.input); got != tc.expected {
			t.Errorf("%q => %q want %q", tc.input, got, tc.expected)
		}
	}
	if !validSnapshotID("valid_0123") || validSnapshotID("invalid/id") || validSnapshotID("x") {
		t.Fatal("incorrect snapshot ID validation")
	}
	if _, err := listSnapshots(context.Background(), defaultEndpoint, exampleToken, 101); err == nil {
		t.Fatal("accepted out-of-range record limit")
	}
}
