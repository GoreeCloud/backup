package backup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testRcloneCapacityAPI(t *testing.T) http.Handler {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test CLI")
	}
	exe := filepath.Join(t.TempDir(), "rclone-stub")
	script := `#!/bin/sh
case "$1" in
  listremotes)
    printf 'local:\nbackup-remote:\nbad:\n'
    ;;
  about)
    [ "$2" = "--json" ] || exit 92
    case "$3" in
      backup-remote:) printf '{"total":10737418240,"used":1073741824,"free":9663676416,"objects":150}\n';;
      bad:) printf '{"total":-1}\n';;
      *) exit 42;;
    esac
    ;;
  *) exit 99;;
esac
`
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	api, err := NewHandler(Config{
		Token: testToken, SourceRoot: t.TempDir(), KopiaBin: "/missing/kopia", RcloneBin: exe,
	})
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func TestRemoteCapacityRequiresConfiguredRemote(t *testing.T) {
	api := testRcloneCapacityAPI(t)
	for _, test := range []struct {
		query string
		want  int
	}{
		{"remote=backup-remote%3A", http.StatusOK},
		{"remote=local%3A", http.StatusNotImplemented},
		{"remote=missing%3A", http.StatusNotFound},
		{"remote=--help", http.StatusBadRequest},
		{"remote=%2Ftmp%2Fprivate", http.StatusBadRequest},
		{"remote=backup-remote%3A&remote=local%3A", http.StatusBadRequest},
		{"", http.StatusBadRequest},
		{"remote=bad%3A", http.StatusBadGateway},
	} {
		r := httptest.NewRequest("GET", "/api/v1/remotes/about?"+test.query, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Errorf("query %q got %d want %d: %s", test.query, w.Code, test.want, w.Body.String())
		}
		if strings.Contains(w.Body.String(), testToken) {
			t.Fatal("credential leaked")
		}
		if test.want == http.StatusOK {
			var response struct {
				Remote string           `json:"remote"`
				Quota  map[string]int64 `json:"quota"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Remote != "backup-remote:" || response.Quota["total"] != 10737418240 || response.Quota["objects"] != 150 {
				t.Errorf("invalid parsed Rclone quota: %+v", response)
			}
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/remotes/about?remote=backup-remote%3A", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated quota read accepted: %d", w.Code)
	}
}

func TestRemoteCapacityConcurrencyGate(t *testing.T) {
	h := &Handler{remoteGate: make(chan struct{}, 1)}
	h.remoteGate <- struct{}{}
	request := httptest.NewRequest("GET", "/api/v1/remotes/about?remote=backup-remote%3A", nil)
	w := httptest.NewRecorder()
	h.remoteCapacity(w, request)
	if w.Code != http.StatusConflict {
		t.Errorf("overlapping lookup got %d", w.Code)
	}
	<-h.remoteGate
}
