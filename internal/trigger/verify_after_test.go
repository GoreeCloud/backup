package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/GoreeCloud/backup/internal/control"
)

type sequencedJobs struct {
	mu       sync.Mutex
	requests []control.Request
	statuses []string
}

func (s *sequencedJobs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+exampleToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/jobs":
		var request control.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		s.requests = append(s.requests, request)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"id":"%024x","status":"queued"}`, len(s.requests))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/jobs/"):
		var index int
		if _, err := fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/"), "%x", &index); err != nil || index < 1 || index > len(s.requests) {
			http.Error(w, "unknown job", http.StatusNotFound)
			return
		}
		status := "completed"
		if index <= len(s.statuses) {
			status = s.statuses[index-1]
		}
		fmt.Fprintf(w, `{"id":"%024x","status":"%s"}`, index, status)
	default:
		http.NotFound(w, r)
	}
}

func TestRunBackupAndVerifyModes(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		ackFull     bool
		expected    string
		expectedAck string
		count       int
	}{
		{"none", false, "", "", 1},
		{"structure", false, "verify-structure", "", 2},
		{"full", true, "verify-full", "VERIFY ALL BACKUPS", 2},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			state := &sequencedJobs{}
			server := httptest.NewServer(state)
			defer server.Close()
			client := Client{BaseURL: server.URL, Token: exampleToken}
			snapshotID, verifyID, err := client.RunBackupAndVerify(context.Background(), t.TempDir(), tc.mode, tc.ackFull)
			if err != nil || snapshotID == "" {
				t.Fatalf("snapshot failed: %q %v", snapshotID, err)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if len(state.requests) != tc.count {
				t.Fatalf("request count: %d != %d", len(state.requests), tc.count)
			}
			if state.requests[0].Type != "backup" {
				t.Errorf("expected backup first; got %+v", state.requests[0])
			}
			if tc.mode == "none" {
				if verifyID != "" {
					t.Errorf("verification job submitted unexpectedly: %s", verifyID)
				}
			} else {
				if verifyID == "" {
					t.Fatal("verification ID missing")
				}
				if state.requests[1].Type != tc.expected || state.requests[1].Confirm != tc.expectedAck {
					t.Errorf("verification payload incorrect: %+v", state.requests[1])
				}
			}
		})
	}
}

func TestRunBackupAndVerifyRejectsInvalidOptionsBeforeSnapshot(t *testing.T) {
	state := &sequencedJobs{}
	server := httptest.NewServer(state)
	defer server.Close()
	client := Client{BaseURL: server.URL, Token: exampleToken}
	for _, tc := range []struct {
		mode string
		ack  bool
	}{
		{"full", false}, {"unknown", false}, {"structure", true}, {"none", true},
	} {
		snapshot, verification, err := client.RunBackupAndVerify(context.Background(), t.TempDir(), tc.mode, tc.ack)
		if err == nil || snapshot != "" || verification != "" {
			t.Errorf("accepted invalid options mode=%s ack=%t: %q %q %v", tc.mode, tc.ack, snapshot, verification, err)
		}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.requests) != 0 {
		t.Fatalf("unsafe invalid options submitted %d API requests", len(state.requests))
	}
}

func TestRunBackupAndVerifyFailureSemantics(t *testing.T) {
	for _, tc := range []struct {
		name        string
		statuses    []string
		wantCount   int
		wantVerify  bool
		errContains string
	}{
		{"backup-fails", []string{"failed"}, 1, false, "backup job ended in status failed"},
		{"verification-fails", []string{"completed", "failed"}, 2, true, "verify-structure job ended in status failed"},
		{"verification-interrupted", []string{"completed", "interrupted"}, 2, true, "verify-structure job ended in status interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &sequencedJobs{statuses: tc.statuses}
			server := httptest.NewServer(state)
			defer server.Close()
			client := Client{BaseURL: server.URL, Token: exampleToken}
			snapshot, verify, err := client.RunBackupAndVerify(context.Background(), t.TempDir(), "structure", false)
			if snapshot == "" || err == nil || !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("bad status: snapshot=%s verification=%s err=%v", snapshot, verify, err)
			}
			if (verify != "") != tc.wantVerify {
				t.Errorf("unexpected verification ID: %q", verify)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if len(state.requests) != tc.wantCount {
				t.Errorf("expected %d operations, got %d", tc.wantCount, len(state.requests))
			}
		})
	}
}
