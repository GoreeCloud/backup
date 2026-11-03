package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	journalSchemaVersion = 1
	maxJournalBytes      = 256 << 10
)

// JobJournal persists only limited job status metadata. In particular it never
// writes engine output, source/destination paths, secrets, or preview approvals.
// It is opt-in and local to the account running GoreeCloud Backup.
type JobJournal struct {
	dir  string
	path string
}

type journalRecord struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type journalFile struct {
	Schema int             `json:"schema"`
	Jobs   []journalRecord `json:"jobs"`
}

func OpenJobJournal(dir string) (*JobJournal, []*Job, error) {
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) == string(filepath.Separator) {
		return nil, nil, errors.New("state directory must be an absolute non-root directory")
	}
	clean := filepath.Clean(dir)
	if info, err := os.Lstat(clean); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(clean, 0700); err != nil {
			return nil, nil, fmt.Errorf("create private state directory: %w", err)
		}
	} else if err != nil {
		return nil, nil, fmt.Errorf("inspect state directory: %w", err)
	} else if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, nil, errors.New("state directory must be an actual private directory (mode 0700)")
	}
	j := &JobJournal{dir: clean, path: filepath.Join(clean, "jobs.json")}
	info, err := os.Lstat(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("inspect state file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxJournalBytes {
		return nil, nil, errors.New("state file must be a private regular file of bounded size")
	}
	file, err := os.Open(j.path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxJournalBytes+1))
	decoder.DisallowUnknownFields()
	var state journalFile
	if err := decoder.Decode(&state); err != nil {
		return nil, nil, fmt.Errorf("state file is invalid: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, nil, errors.New("state file must contain exactly one JSON object")
	}
	if state.Schema != journalSchemaVersion || len(state.Jobs) > 100 {
		return nil, nil, errors.New("unsupported state format or invalid history length")
	}
	recovered := make([]*Job, 0, len(state.Jobs))
	seen := make(map[string]bool, len(state.Jobs))
	interrupted := false
	for _, record := range state.Jobs {
		if record.ID == "" || len(record.ID) > 64 || seen[record.ID] || record.Type == "" || record.StartedAt.IsZero() {
			return nil, nil, errors.New("state file contains invalid job metadata")
		}
		seen[record.ID] = true
		switch record.Status {
		case "queued", "running", "cancelling":
			record.Status = "interrupted"
			now := time.Now().UTC()
			record.CompletedAt = &now
			interrupted = true
		case "completed", "failed", "cancelled", "interrupted":
		default:
			return nil, nil, errors.New("state file contains unsupported job status")
		}
		recovered = append(recovered, &Job{
			ID: record.ID, Type: record.Type, Status: record.Status,
			StartedAt: record.StartedAt, CompletedAt: record.CompletedAt, Error: record.Error,
		})
	}
	if interrupted {
		if err := j.Persist(recovered); err != nil {
			return nil, nil, fmt.Errorf("record interrupted jobs: %w", err)
		}
	}
	return j, recovered, nil
}

func (j *JobJournal) Persist(jobs []*Job) error {
	if j == nil {
		return nil
	}
	state := journalFile{Schema: journalSchemaVersion, Jobs: make([]journalRecord, 0, len(jobs))}
	for _, job := range jobs {
		state.Jobs = append(state.Jobs, journalRecord{
			ID: job.ID, Type: job.Type, Status: job.Status,
			StartedAt: job.StartedAt, CompletedAt: job.CompletedAt, Error: job.Error,
		})
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxJournalBytes {
		return errors.New("job history exceeds state file size limit")
	}
	file, err := os.CreateTemp(j.dir, ".jobs-*.tmp")
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
	if err := os.Rename(file.Name(), j.path); err != nil {
		return err
	}
	// Sync the containing directory so the rename is durable after power loss
	// on filesystems that support directory fsync.
	directory, err := os.Open(j.dir)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
