package control

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Verification uses fixed Kopia subcommands; no user-supplied command flags.
func TestVerificationCommands(t *testing.T) {
	cases := []struct {
		request Request
		want []string
		rejected bool
	}{
		{Request{Type: "verify-structure"}, []string{"snapshot", "verify", "--verify-files-percent=0", "--file-parallelism=2"}, false},
		{Request{Type: "verify-full"}, nil, true},
		{Request{Type: "verify-full", Confirm: "verify all backups"}, nil, true},
		{Request{Type: "verify-full", Confirm: "VERIFY ALL BACKUPS"}, []string{"snapshot", "verify", "--verify-files-percent=100", "--file-parallelism=2"}, false},
	}
	for _, tc := range cases {
		cmd, err := BuildCommand(tc.request)
		if tc.rejected {
			if err == nil { t.Fatalf("accepted unconfirmed verification: %+v", tc.request) }
			continue
		}
		if err != nil { t.Fatalf("verification rejected: %v", err) }
		if cmd.Binary != "kopia" || !reflect.DeepEqual(cmd.Args, tc.want) {
			t.Fatalf("unexpected verification command: %+v", cmd)
		}
	}
}

func TestVerificationAPIAuthorizationAndConfirmation(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kopia"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil { t.Fatal(err) }
	t.Setenv("PATH", bin)
	srv := NewServer(testToken, &fakeExecutor{})
	submit := func(request Request, bearer string) int {
		t.Helper()
		payload, err := json.Marshal(request)
		if err != nil { t.Fatal(err) }
		httpRequest := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(string(payload)))
		if bearer != "" { httpRequest.Header.Set("Authorization", "Bearer "+bearer) }
		httpRequest.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httpRequest)
		return rec.Code
	}
	if got := submit(Request{Type: "verify-structure"}, ""); got != 401 {
		t.Fatalf("unauthorized verification returned %d", got)
	}
	if got := submit(Request{Type: "verify-full"}, testToken); got != 422 {
		t.Fatalf("unconfirmed full verification returned %d", got)
	}
	if got := submit(Request{Type: "verify-structure"}, testToken); got != 202 {
		t.Fatalf("structural verification returned %d", got)
	}
	if got := submit(Request{Type: "verify-full", Confirm: "VERIFY ALL BACKUPS"}, testToken); got != 202 {
		t.Fatalf("confirmed full verification returned %d", got)
	}
}
