package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policy(source string) Policy {
	return Policy{
		ID: "documents", Enabled: true, Source: source,
		IntervalMinutes: 60, OffsetMinutes: 15,
		VerifyAfter: "none",
	}
}
func fixture(t *testing.T) Configuration {
	t.Helper()
	return Configuration{Schema: SchemaVersion, Policies: []Policy{policy(t.TempDir())}}
}

func TestValidatePolicies(t *testing.T) {
	valid := fixture(t)
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Configuration)
	}{
		{"schema", func(c *Configuration) { c.Schema = 2 }},
		{"duplicate-id", func(c *Configuration) { c.Policies = append(c.Policies, c.Policies[0]) }},
		{"root-source", func(c *Configuration) { c.Policies[0].Source = "/" }},
		{"relative", func(c *Configuration) { c.Policies[0].Source = "Documents" }},
		{"interval-low", func(c *Configuration) { c.Policies[0].IntervalMinutes = 59 }},
		{"interval-high", func(c *Configuration) { c.Policies[0].IntervalMinutes = 10081 }},
		{"offset-bound", func(c *Configuration) { c.Policies[0].OffsetMinutes = 60 }},
		{"full-no-ack", func(c *Configuration) { c.Policies[0].VerifyAfter = "full" }},
		{"ack-wrong-mode", func(c *Configuration) { c.Policies[0].AcknowledgeFull = true }},
		{"unknown-mode", func(c *Configuration) { c.Policies[0].VerifyAfter = "repair" }},
		{"empty-id", func(c *Configuration) { c.Policies[0].ID = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			p.Policies = append([]Policy(nil), valid.Policies...)
			tc.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatalf("accepted invalid config: %+v", p)
			}
		})
	}
	valid.Policies[0].VerifyAfter = "full"
	valid.Policies[0].AcknowledgeFull = true
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateConfigFileAndStrictJSON(t *testing.T) {
	cfg := fixture(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.json")
	write := func(content []byte) {
		t.Helper()
		if err := os.WriteFile(file, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	write(contents)
	if _, err := LoadConfiguration(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfiguration(file); err == nil {
		t.Fatal("accepted public config")
	}
	if err := os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Symlink(file, alias); err == nil {
		if _, err := LoadConfiguration(alias); err == nil {
			t.Fatal("accepted symlinked policy")
		}
	}
	write(append(contents, []byte(" {}")...))
	if _, err := LoadConfiguration(file); err == nil {
		t.Fatal("accepted multiple JSON values")
	}
	write([]byte(`{"schema":1,"policies":[],"unknown":"field"}`))
	if _, err := LoadConfiguration(file); err == nil {
		t.Fatal("accepted unexpected config field")
	}
	write([]byte(strings.Repeat(" ", MaxConfigBytes+1)))
	if _, err := LoadConfiguration(file); err == nil {
		t.Fatal("accepted oversized config")
	}
	if _, err := LoadConfiguration("relative.json"); err == nil {
		t.Fatal("accepted relative config")
	}
}

func TestUTCSlotAndMissedWindows(t *testing.T) {
	p := policy("/tmp/example")
	base := time.Date(2026, 11, 3, 13, 15, 0, 0, time.UTC)
	for _, tc := range []struct {
		offset time.Duration
		want   bool
		start  time.Time
	}{
		{time.Minute, true, base},
		{9*time.Minute + 59*time.Second, true, base},
		{10 * time.Minute, false, base},
		{45 * time.Minute, false, base},
		{time.Hour + time.Minute, true, base.Add(time.Hour)},
	} {
		slot, due := SlotStart(p, base.Add(tc.offset))
		if due != tc.want || !slot.Equal(tc.start) {
			t.Errorf("at %v: due=%t slot=%v want=%t %v", tc.offset, due, slot, tc.want, tc.start)
		}
	}
	// UTC and US daylight saving offset do not change the schedule.
	local := time.FixedZone("UTC-5", -5*3600)
	s1, _ := SlotStart(p, base.In(local).Add(time.Minute))
	if !s1.Equal(base) {
		t.Fatalf("timezone altered UTC slot: %v", s1)
	}
}

func TestDurableClaimNoReplayAndCrashRecovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Linux flock prevents a second simultaneous one-shot scheduler.
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("second process acquired active lock")
	}
	slot := time.Date(2026, 11, 3, 14, 15, 0, 0, time.UTC)
	ok, err := s.Claim("documents", slot)
	if err != nil || !ok {
		t.Fatalf("claim: %v %t", err, ok)
	}
	ok, err = s.Claim("documents", slot)
	if err != nil || ok {
		t.Fatalf("duplicate claim allowed: %v %t", err, ok)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, _ := s.Record("documents")
	if rec.Status != "interrupted" || rec.LastSlotUnix != slot.Unix() {
		t.Fatalf("inflight restart was not marked interrupted: %+v", rec)
	}
	ok, err = s.Claim("documents", slot)
	if err != nil || ok {
		t.Fatalf("crash caused duplicate in same slot: %v %t", err, ok)
	}
	next := slot.Add(time.Hour)
	ok, err = s.Claim("documents", next)
	if err != nil || !ok {
		t.Fatalf("next scheduled slot was blocked: %v %t", err, ok)
	}
	if err := s.Finish("documents", next, "abc123", "", nil); err != nil {
		t.Fatal(err)
	}
	rec, _ = s.Record("documents")
	if rec.Status != "completed" || rec.SnapshotID != "abc123" {
		t.Fatalf("completed status not durable: %+v", rec)
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "schedule-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bytes), `"status":"completed"`) {
		t.Fatal("completed state missing")
	}
	if strings.Contains(string(bytes), "/tmp/") || strings.Contains(string(bytes), "secret") {
		t.Fatal("schedule state leaked source path or secrets")
	}
	if st, err := os.Stat(filepath.Join(dir, "schedule-state.json")); err != nil || st.Mode().Perm()&0077 != 0 {
		t.Fatalf("state file not private: %v %v", st, err)
	}
}

func TestRunDueAtMostOnceAndFailure(t *testing.T) {
	cfg := fixture(t)
	now := time.Date(2026, 11, 3, 13, 16, 0, 0, time.UTC)
	store, err := OpenStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	run := func(ctx context.Context, p Policy) (string, string, error) {
		calls++
		if p.ID != "documents" {
			t.Fatalf("wrong policy: %s", p.ID)
		}
		return "test-snapshot", "", nil
	}
	results, err := RunDue(context.Background(), cfg, store, func() time.Time { return now }, run)
	if err != nil || len(results) != 1 || results[0].Outcome != "completed" || calls != 1 {
		t.Fatalf("unexpected first run: %+v err=%v calls=%d", results, err, calls)
	}
	results, err = RunDue(context.Background(), cfg, store, func() time.Time { return now.Add(2 * time.Minute) }, run)
	if err != nil || len(results) != 0 || calls != 1 {
		t.Fatalf("duplicate within same slot: %+v err=%v calls=%d", results, err, calls)
	}
	results, err = RunDue(context.Background(), cfg, store, func() time.Time { return now.Add(15 * time.Minute) }, run)
	if err != nil || len(results) != 0 || calls != 1 {
		t.Fatalf("unexpected catch-up run: %+v err=%v calls=%d", results, err, calls)
	}
	fail := func(context.Context, Policy) (string, string, error) {
		calls++
		return "snap2", "", errors.New("synthetic error")
	}
	results, err = RunDue(context.Background(), cfg, store, func() time.Time { return now.Add(time.Hour) }, fail)
	if err == nil || len(results) != 1 || results[0].Outcome != "failed" || calls != 2 {
		t.Fatalf("failed job was not recorded: %+v err=%v calls=%d", results, err, calls)
	}
	rec, _ := store.Record("documents")
	if rec.Status != "failed" {
		t.Fatalf("failed status not persisted: %+v", rec)
	}
	results, err = RunDue(context.Background(), cfg, store, func() time.Time { return now.Add(time.Hour + 2*time.Minute) }, run)
	if err != nil || len(results) != 0 || calls != 2 {
		t.Fatal("failed job was retried within same slot")
	}
}

func TestDisabledExpiredAndInvalidSourceDoNotSubmit(t *testing.T) {
	cfg := fixture(t)
	now := time.Date(2026, 11, 3, 13, 16, 0, 0, time.UTC)
	store, err := OpenStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	fn := func(context.Context, Policy) (string, string, error) { calls++; return "", "", nil }
	cfg.Policies[0].Enabled = false
	if results, err := RunDue(context.Background(), cfg, store, func() time.Time { return now }, fn); err != nil || len(results) != 0 {
		t.Fatalf("disabled policy attempted: %+v %v", results, err)
	}
	cfg.Policies[0].Enabled = true
	if results, err := RunDue(context.Background(), cfg, store, func() time.Time { return now.Add(20 * time.Minute) }, fn); err != nil || len(results) != 0 {
		t.Fatalf("missed window replayed: %+v %v", results, err)
	}
	cfg.Policies[0].Source = filepath.Join(t.TempDir(), "missing")
	if results, err := RunDue(context.Background(), cfg, store, func() time.Time { return now }, fn); err == nil || len(results) != 1 || results[0].Outcome != "invalid-source" {
		t.Fatalf("invalid source accepted: %+v %v", results, err)
	}
	if calls != 0 {
		t.Fatal("unexpected external API invocation")
	}
	if _, ok := store.Record("documents"); ok {
		t.Fatal("failed validation consumed schedule slot")
	}
}

func TestStateRefusesSymlinksOrCorruption(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target")
	if err := os.WriteFile(target, []byte("untrusted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, "schedule-state.json")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := OpenStore(directory); err == nil {
		t.Fatal("accepted symlink state file")
	}
	if err := os.Remove(filepath.Join(directory, "schedule-state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "schedule-state.json"), []byte(`{"schema":2,"records":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(directory); err == nil {
		t.Fatal("accepted unknown state schema")
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(directory); err == nil {
		t.Fatal("accepted readable state directory")
	}
}

func TestRunDuePropagatesCancellation(t *testing.T) {
	cfg := fixture(t)
	s, err := OpenStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := RunDue(ctx, cfg, s, func() time.Time { return time.Now() }, func(context.Context, Policy) (string, string, error) {
		t.Fatal("executed despite cancellation")
		return "", "", nil
	})
	if !errors.Is(err, context.Canceled) || len(out) != 0 {
		t.Fatalf("unexpected cancelled outcome: %+v %v", out, err)
	}
}

func TestMissingInitializedLedgerFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(dir, "schedule-state.json")
	if _, err := os.Stat(ledger); err != nil {
		t.Fatalf("first run did not create ledger: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ledger); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err == nil || !strings.Contains(err.Error(), "missing after prior initialization") {
		t.Fatalf("ledger loss was silently recreated: %v", err)
	}
}
