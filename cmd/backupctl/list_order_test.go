package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSnapshotListUsesTimestampsNotProviderListOrder(t *testing.T) {
	rows := []map[string]any{
		{"id": "point0001", "startTime": "2026-11-17T11:00:00Z", "source": map[string]any{"path": "/private/old"}},
		{"id": "point0002", "startTime": "2026-11-19T15:00:00Z", "source": map[string]any{"path": "/private/recent"}},
		{"id": "point0003", "startTime": "bad-date", "source": map[string]any{"path": "/private/unknown"}},
		{"id": "point0002", "startTime": "2026-11-18T13:00:00Z", "source": map[string]any{"path": "/private/stale-duplicate"}},
		{"id": "point0004", "startTime": "2026-11-19T13:00:00Z", "source": map[string]any{"path": "/private/earlier"}},
		{"id": "unsafe/id", "startTime": "2026-11-20T00:00:00Z", "source": map[string]any{"path": "/private/invalid"}},
	}
	counter := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/snapshots" ||
			r.Header.Get("Authorization") != "Bearer "+exampleToken {
			t.Errorf("non-read-only or unauthorized request")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(rows); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	got, err := listSnapshots(context.Background(), server.URL, exampleToken, 100)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"point0002", "point0004", "point0001", "point0003"}
	if len(got) != len(expected) {
		t.Fatalf("unexpected snapshot count: %+v", got)
	}
	for index, id := range expected {
		if got[index].ID != id {
			t.Errorf("at %d got %q want %q", index, got[index].ID, id)
		}
	}
	if got[0].Label != "recent" {
		t.Errorf("duplicate selected older record: %+v", got[0])
	}
	if got[3].Timestamp != "unknown" {
		t.Errorf("invalid timestamp should be last: %+v", got[3])
	}
	limited, err := listSnapshots(context.Background(), server.URL, exampleToken, 2)
	if err != nil || len(limited) != 2 || limited[0].ID != expected[0] || limited[1].ID != expected[1] {
		t.Errorf("snapshot limit/sort incorrect: %+v %v", limited, err)
	}
	if counter != 2 {
		t.Errorf("unexpected number of metadata GETs: %d", counter)
	}
}

func TestSnapshotListStableOrderForTiesAndUndatedRecords(t *testing.T) {
	entries := []map[string]any{
		{"id": "tie00001", "startTime": "2026-11-19T17:00:00Z", "source": map[string]any{"path": "First"}},
		{"id": "tie00002", "startTime": "2026-11-19T17:00:00Z", "source": map[string]any{"path": "Second"}},
		{"id": "bad00001", "startTime": "bogus", "source": map[string]any{"path": "Invalid"}},
		{"id": "bad00002", "startTime": "bogus", "source": map[string]any{"path": "Unknown"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(entries)
	}))
	defer server.Close()
	result, err := listSnapshots(context.Background(), server.URL, exampleToken, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tie00002", "tie00001", "bad00002", "bad00001"}
	if len(result) != len(want) {
		t.Fatalf("unexpected summaries: %+v", result)
	}
	for i, id := range want {
		if result[i].ID != id {
			t.Errorf("tie order %d: %q want %q", i, result[i].ID, id)
		}
	}
}

func TestSnapshotListRejectsBidiOrMarkupSourceLabels(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/home/private/\u202eSpoof.txt", "Protected snapshot"},
		{"/home/private/\u2066Spoof.txt", "Protected snapshot"},
		{"/home/private/<script>", "Protected snapshot"},
		{"/home/private/file\nname", "filename"},
		{"/home/private/Grüße-東京.txt", "Grüße-東京.txt"},
		{strings.Repeat("a", 4097), "Protected snapshot"},
	} {
		if got := safeSnapshotDisplay(tc.input); got != tc.want {
			t.Errorf("unexpected path label for synthetic fixture: got %q want %q", got, tc.want)
		}
	}
}
