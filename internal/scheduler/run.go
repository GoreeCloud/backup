package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GoreeCloud/backup/internal/control"
)

type Runner func(context.Context, Policy) (snapshotID, verifyID string, err error)

type Result struct {
	PolicyID string
	Outcome  string
}

// RunDue performs only due, explicitly enabled policies. It preserves a
// fsynced at-most-once claim per UTC slot, and never replays missed windows.
func RunDue(ctx context.Context, cfg Configuration, store *Store, clock func() time.Time, run Runner) ([]Result, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var results []Result
	var failures []error
	for _, p := range cfg.Policies {
		if ctx.Err() != nil {
			return results, errors.Join(append(failures, ctx.Err())...)
		}
		if !p.Enabled {
			continue
		}
		slot, inWindow := SlotStart(p, clock())
		if !inWindow {
			continue
		}
		if rec, exists := store.Record(p.ID); exists && rec.LastSlotUnix >= slot.Unix() {
			continue
		}
		// Ensure the configured source is currently present and safe before
		// writing the durable claim; failed validation can be corrected in
		// the remaining time window.
		if _, err := control.BuildCommand(control.Request{Type: "backup", Source: p.Source}); err != nil {
			results = append(results, Result{p.ID, "invalid-source"})
			failures = append(failures, fmt.Errorf("policy %s: source validation failed", p.ID))
			continue
		}
		claimed, err := store.Claim(p.ID, slot)
		if err != nil {
			results = append(results, Result{p.ID, "state-error"})
			failures = append(failures, fmt.Errorf("policy %s: durable claim failed: %w", p.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		snapshotID, verifyID, runErr := run(ctx, p)
		outcome := "completed"
		if runErr != nil {
			outcome = "failed"
			failures = append(failures, fmt.Errorf("policy %s: backup or verification failed: %w", p.ID, runErr))
		}
		if err := store.Finish(p.ID, slot, snapshotID, verifyID, runErr); err != nil {
			outcome = "status-error"
			failures = append(failures, fmt.Errorf("policy %s: save result failed: %w", p.ID, err))
		}
		results = append(results, Result{p.ID, outcome})
	}
	return results, errors.Join(failures...)
}
