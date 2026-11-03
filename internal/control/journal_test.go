package control

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalPersistsOnlySafeJobMetadata(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "kopia"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	privateDir := t.TempDir()
	if err := os.Chmod(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServerWithState(testToken, &fakeExecutor{}, privateDir)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	payload, _ := json.Marshal(Request{Type: "backup", Source: source})
	req := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(string(payload)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	srv.ServeHTTP(response, req)
	if response.Code != 202 {
		t.Fatalf("job acceptance: %d %s", response.Code, response.Body.String())
	}
	for i := 0; i < 100; i++ {
		check := httptest.NewRequest("GET", "/api/v1/jobs", nil)
		check.Header.Set("Authorization", "Bearer "+testToken)
		out := httptest.NewRecorder()
		srv.ServeHTTP(out, check)
		if strings.Contains(out.Body.String(), `"status":"completed"`) {
			break
		}
		if i == 99 {
			t.Fatal("job never completed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	stored, err := os.ReadFile(filepath.Join(privateDir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(privateDir, "jobs.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("journal must be mode 0600: %v %v", info, err)
	}
	for _, forbidden := range []string{source, testToken, "fake completed", "source", "destination", "output", "preview_used"} {
		if strings.Contains(string(stored), forbidden) {
			t.Fatalf("journal persisted prohibited data: %s", forbidden)
		}
	}
	var state journalFile
	if err := json.Unmarshal(stored, &state); err != nil || len(state.Jobs) != 1 || state.Jobs[0].Status != "completed" {
		t.Fatalf("unexpected saved state: %v %+v", err, state)
	}
	fresh, err := NewServerWithState(testToken, &fakeExecutor{}, privateDir)
	if err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	list.Header.Set("Authorization", "Bearer "+testToken)
	res := httptest.NewRecorder()
	fresh.ServeHTTP(res, list)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"completed"`) || strings.Contains(res.Body.String(), "fake completed") {
		t.Fatalf("stored job not recovered correctly: %d %s", res.Code, res.Body.String())
	}
}

func TestJournalMarksInFlightJobsInterrupted(t *testing.T) {
	privateDir := t.TempDir()
	if err := os.Chmod(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	initial := journalFile{Schema: journalSchemaVersion, Jobs: []journalRecord{
		{ID: "test-job-id", Type: "backup", Status: "running", StartedAt: now},
	}}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(filepath.Join(privateDir, "jobs.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, recovered, err := OpenJobJournal(privateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].Status != "interrupted" || recovered[0].CompletedAt == nil {
		t.Fatalf("interrupted job not marked: %+v", recovered)
	}
	persisted, _ := os.ReadFile(filepath.Join(privateDir, "jobs.json"))
	if !strings.Contains(string(persisted), `"status":"interrupted"`) {
		t.Fatalf("recovery state not persisted: %s", persisted)
	}
}

func TestJournalRejectsInsecureOrCorruptFiles(t *testing.T) {
	worldReadableDir := t.TempDir()
	if err := os.Chmod(worldReadableDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenJobJournal(worldReadableDir); err == nil {
		t.Fatal("world-readable journal directory accepted")
	}
	for _, tc := range []struct {
		name    string
		content []byte
		perm    os.FileMode
	}{
		{name: "corrupt", content: []byte("not-json"), perm: 0600},
		{name: "world-readable", content: []byte(`{"schema":1,"jobs":[]}`), perm: 0644},
		{name: "unsupported-version", content: []byte(`{"schema":500,"jobs":[]}`), perm: 0600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			privateDir := t.TempDir()
			if err := os.Chmod(privateDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(privateDir, "jobs.json"), tc.content, tc.perm); err != nil {
				t.Fatal(err)
			}
			if _, _, err := OpenJobJournal(privateDir); err == nil {
				t.Fatal("invalid journal was accepted")
			}
		})
	}
}

func TestJournalDoesNotCarrySyncPreviewAuthorizationAcrossRestart(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	j, _, err := OpenJobJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job := &Job{ID: "historic-preview", Type: "sync-preview", Status: "completed", StartedAt: now, CompletedAt: &now, Source: "remote:source", Destination: "remote:dest"}
	if err := j.Persist([]*Job{job}); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "rclone"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	fresh, err := NewServerWithState(testToken, &fakeExecutor{}, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"type":"sync","source":"remote:source","destination":"remote:dest","confirm":"DELETE DESTINATION EXTRAS"}`
	req := httptest.NewRequest("POST", "http://localhost:8787/api/v1/jobs", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	fresh.ServeHTTP(res, req)
	if res.Code != 409 {
		t.Fatalf("old preview could authorize sync after restart: %d %s", res.Code, res.Body.String())
	}
}
