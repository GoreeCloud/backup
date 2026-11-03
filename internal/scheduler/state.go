package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const MaxStateBytes = 64 << 10

type Record struct {
	LastSlotUnix int64      `json:"last_slot_unix"`
	Status       string     `json:"status"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	SnapshotID   string     `json:"snapshot_id,omitempty"`
	VerifyID     string     `json:"verify_id,omitempty"`
}

type stateFile struct {
	Schema  int               `json:"schema"`
	Records map[string]Record `json:"records"`
}

// Store is exclusively held by a single Linux scheduler process, backed by
// a private status-only atomic file. It never serializes source file paths,
// bearer tokens, engine output, or repository credentials.
type Store struct {
	dir  string
	lock *os.File
	data stateFile
}

func OpenStore(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) == string(filepath.Separator) {
		return nil, errors.New("state path must be absolute and non-root")
	}
	dir = filepath.Clean(dir)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.Mkdir(dir, 0700); err != nil {
			return nil, fmt.Errorf("create private schedule state: %w", err)
		}
		info, err = os.Lstat(dir)
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("schedule state directory must be a real owner-only directory")
	}

	// O_NOFOLLOW and flock defend against accidental aliases and concurrently
	// invoked timer processes. A crash releases flock without deleting the file.
	lockName := filepath.Join(dir, ".schedule.lock")
	// Distinguish genuine first-run initialization from a disappeared ledger.
	// Recreating an absent ledger after a prior invocation could repeat work.
	lock, err := os.OpenFile(lockName, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	firstRun := err == nil
	if errors.Is(err, os.ErrExist) {
		lock, err = os.OpenFile(lockName, os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	}
	if err != nil {
		return nil, fmt.Errorf("open schedule lock: %w", err)
	}
	st, err := lock.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		lock.Close()
		return nil, errors.New("schedule lock is not a private regular file")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another schedule invocation is active")
	}
	s := &Store{dir: dir, lock: lock,
		data: stateFile{Schema: SchemaVersion, Records: map[string]Record{}}}
	data, err := PrivateFile(filepath.Join(dir, "schedule-state.json"), MaxStateBytes)
	if errors.Is(err, os.ErrNotExist) {
		if !firstRun {
			s.Close()
			return nil, errors.New("schedule ledger is missing after prior initialization; review existing backups before manual recovery")
		}
		// Atomically install an empty versioned ledger before allowing the
		// first claim. A crashed first-run initialization then fails closed.
		if err := s.save(); err != nil {
			s.Close()
			return nil, fmt.Errorf("initialize schedule ledger: %w", err)
		}
		return s, nil
	}
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("schedule state unavailable: %w", err)
	}
	if err := decodeOne(data, &s.data); err != nil {
		s.Close()
		return nil, err
	}
	if s.data.Schema != SchemaVersion || s.data.Records == nil || len(s.data.Records) > 64 {
		s.Close()
		return nil, errors.New("unsupported schedule state")
	}
	changed := false
	for id, record := range s.data.Records {
		if !policyID.MatchString(id) || record.LastSlotUnix < 0 || record.Status == "" {
			s.Close()
			return nil, errors.New("invalid schedule state entry")
		}
		switch record.Status {
		case "claimed":
			record.Status = "interrupted"
			now := time.Now().UTC()
			record.CompletedAt = &now
			s.data.Records[id] = record
			changed = true
		case "completed", "failed", "interrupted":
		default:
			s.Close()
			return nil, errors.New("unsupported schedule outcome")
		}
	}
	if changed {
		if err := s.save(); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.lock == nil {
		return nil
	}
	err := syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	closeErr := s.lock.Close()
	s.lock = nil
	if err != nil {
		return err
	}
	return closeErr
}

func (s *Store) Record(id string) (Record, bool) {
	v, ok := s.data.Records[id]
	return v, ok
}

// Claim is persisted and fsynced *before* submitting the snapshot. Crashes
// skip an uncertain outcome in the same slot rather than replay it.
func (s *Store) Claim(id string, start time.Time) (bool, error) {
	if !policyID.MatchString(id) {
		return false, errors.New("invalid policy ID")
	}
	slot := start.UTC().Unix()
	if previous, ok := s.Record(id); ok && previous.LastSlotUnix >= slot {
		return false, nil
	}
	if len(s.data.Records) >= 64 {
		if _, exists := s.data.Records[id]; !exists {
			return false, errors.New("schedule state record limit reached")
		}
	}
	prior, existed := s.data.Records[id]
	s.data.Records[id] = Record{LastSlotUnix: slot, Status: "claimed"}
	if err := s.save(); err != nil {
		if existed {
			s.data.Records[id] = prior
		} else {
			delete(s.data.Records, id)
		}
		return false, err
	}
	return true, nil
}

func (s *Store) Finish(id string, slot time.Time, snapshotID, verifyID string, runErr error) error {
	rec, ok := s.data.Records[id]
	if !ok || rec.LastSlotUnix != slot.UTC().Unix() || rec.Status != "claimed" {
		return errors.New("schedule slot was not claimed")
	}
	rec.Status = "completed"
	if runErr != nil {
		rec.Status = "failed"
	}
	now := time.Now().UTC()
	rec.CompletedAt = &now
	rec.SnapshotID, rec.VerifyID = snapshotID, verifyID
	s.data.Records[id] = rec
	return s.save()
}

func (s *Store) save() error {
	data, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if len(data) > MaxStateBytes {
		return errors.New("schedule state exceeds size limit")
	}
	file, err := os.CreateTemp(s.dir, ".schedule-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(s.dir, "schedule-state.json")); err != nil {
		return err
	}
	d, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
