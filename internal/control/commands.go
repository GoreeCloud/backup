package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

type Executor interface {
	Run(ctx context.Context, binary string, args ...string) (string, error)
}

type OSExecutor struct{}

type cappedBuffer struct {
	mu    sync.Mutex
	bytes []byte
	max   int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n := b.max - len(b.bytes); n > 0 {
		if n > len(p) {
			n = len(p)
		}
		b.bytes = append(b.bytes, p[:n]...)
	}
	return len(p), nil
}

func (OSExecutor) Run(ctx context.Context, binary string, args ...string) (string, error) {
	if binary != "kopia" && binary != "rclone" {
		return "", errors.New("engine not permitted")
	}
	// Kopia's default restore can overwrite existing data. Reserve the new
	// destination atomically at execution time, not only at request validation.
	if binary == "kopia" && len(args) >= 4 && args[0] == "snapshot" && args[1] == "restore" {
		if err := os.Mkdir(args[len(args)-1], 0700); err != nil {
			return "", fmt.Errorf("restore destination is no longer available: %w", err)
		}
	}
	// Recheck paths immediately before launch: a symlink may have changed
	// since the API accepted the job. This narrows, but cannot eliminate,
	// filesystem TOCTOU races while an external engine is running.
	if binary == "rclone" && len(args) >= 3 {
		switch args[0] {
		case "copy", "check", "sync":
			overlap, err := locationsOverlap(args[1], args[2])
			if err != nil {
				return "", err
			}
			if overlap {
				return "", errors.New("source and destination overlap at execution time")
			}
			if args[0] == "sync" {
				if err := safeSyncDestination(args[2]); err != nil {
					return "", err
				}
			}
		}
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	// Child processes need repository credentials, never the management API token.
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GOREECLOUD_BACKUP_TOKEN=") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	output := &cappedBuffer{max: 12 << 10}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	if err != nil {
		return string(output.bytes), fmt.Errorf("%s operation failed: %w", binary, err)
	}
	return string(output.bytes), nil
}

type Request struct {
	Type        string `json:"type"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	SnapshotID  string `json:"snapshot_id,omitempty"`
	Confirm     string `json:"confirm,omitempty"`
}

type Command struct {
	Binary string
	Args   []string
}

func validateOperand(s string) error {
	if s == "" || len(s) > 2048 || strings.HasPrefix(s, "-") {
		return errors.New("path is empty, too long, or begins with an option")
	}
	for _, r := range s {
		if r == 0 || unicode.IsControl(r) {
			return errors.New("control characters are prohibited")
		}
	}
	return nil
}

func localDirectory(path string) (string, error) {
	if err := validateOperand(path); err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) == string(filepath.Separator) {
		return "", errors.New("source must be an absolute non-root directory")
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved == string(filepath.Separator) {
		return "", errors.New("source cannot be a symlink to filesystem root")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", errors.New("source directory was not found")
	}
	return filepath.Clean(path), nil
}

func restoreDestination(path string) (string, error) {
	if err := validateOperand(path); err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) == string(filepath.Separator) {
		return "", errors.New("destination must be an absolute non-root directory")
	}
	clean := filepath.Clean(path)
	_, err := os.Lstat(clean)
	if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("restore destination must not exist to prevent overwrites")
	}
	parent, err := os.Stat(filepath.Dir(clean))
	if err != nil || !parent.IsDir() {
		return "", errors.New("restore parent directory must exist")
	}
	return clean, nil
}

func remoteOrAbsolute(path string) error {
	if err := validateOperand(path); err != nil {
		return err
	}
	if !filepath.IsAbs(path) && !strings.Contains(path, ":") {
		return errors.New("provide an absolute path or configured Rclone remote")
	}
	return nil
}

// safeSyncDestination prevents broad accidental deletion, including alternate
// spellings of local and remote storage roots and symlinks to local roots.
func safeSyncDestination(destination string) error {
	if filepath.IsAbs(destination) {
		cleaned := filepath.Clean(destination)
		if cleaned == string(filepath.Separator) {
			return errors.New("sync to local filesystem root is prohibited")
		}
		resolved, err := canonicalLocalPath(cleaned)
		if err != nil {
			return err
		}
		if resolved == string(filepath.Separator) {
			return errors.New("sync to symlinked filesystem root is prohibited")
		}
		return nil
	}
	colon := strings.IndexByte(destination, ':')
	if colon < 1 {
		return errors.New("invalid remote destination")
	}
	// Treat remote:/folder/.. and remote:./ as roots as well.
	if path.Clean("/"+destination[colon+1:]) == "/" {
		return errors.New("sync to remote storage root is prohibited")
	}
	return nil
}

// canonicalLocalPath resolves the existing prefix of an absolute local path.
// Unlike EvalSymlinks(path), it also supports not-yet-created destination
// directories. Dangling and cyclic symlinks fail closed.
func canonicalLocalPath(input string) (string, error) {
	current := filepath.Clean(input)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("cannot resolve local path: %w", err)
		}
		// An existing symlink with a nonexistent target must not be
		// mistaken for an ordinary absent destination directory.
		if _, lerr := os.Lstat(current); lerr == nil {
			return "", errors.New("local path contains a dangling symlink")
		} else if !errors.Is(lerr, os.ErrNotExist) {
			return "", fmt.Errorf("cannot inspect local path: %w", lerr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("could not resolve local path")
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func locationsOverlap(a, b string) (bool, error) {
	if filepath.IsAbs(a) && filepath.IsAbs(b) {
		var err error
		a, err = canonicalLocalPath(a)
		if err != nil {
			return false, err
		}
		b, err = canonicalLocalPath(b)
		if err != nil {
			return false, err
		}
		return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) ||
			strings.HasPrefix(b, a+string(filepath.Separator)), nil
	}
	ca, cb := strings.IndexByte(a, ':'), strings.IndexByte(b, ':')
	if ca < 1 || cb < 1 || a[:ca] != b[:cb] {
		return false, nil
	}
	cleanA := path.Clean("/" + a[ca+1:])
	cleanB := path.Clean("/" + b[cb+1:])
	return cleanA == cleanB || strings.HasPrefix(cleanA, cleanB+"/") ||
		strings.HasPrefix(cleanB, cleanA+"/"), nil
}

func BuildCommand(r Request) (Command, error) {
	switch r.Type {
	case "backup":
		source, err := localDirectory(r.Source)
		if err != nil {
			return Command{}, err
		}
		return Command{"kopia", []string{"snapshot", "create", source}}, nil
	case "snapshots":
		return Command{"kopia", []string{"snapshot", "list", "--json"}}, nil
	case "verify-structure":
		// Structural verification walks repository snapshot metadata and blobs.
		// It does not download and decrypt all file contents.
		return Command{"kopia", []string{"snapshot", "verify", "--verify-files-percent=0", "--file-parallelism=2"}}, nil
	case "verify-full":
		// A complete content verification can download the entire repository.
		// Require explicit operator acknowledgment of bandwidth/egress costs.
		if r.Confirm != "VERIFY ALL BACKUPS" {
			return Command{}, errors.New("full verification requires typed confirmation (VERIFY ALL BACKUPS)")
		}
		return Command{"kopia", []string{"snapshot", "verify", "--verify-files-percent=100", "--file-parallelism=2"}}, nil
	case "copy", "check", "sync-preview", "sync":
		if err := remoteOrAbsolute(r.Source); err != nil {
			return Command{}, err
		}
		if err := remoteOrAbsolute(r.Destination); err != nil {
			return Command{}, err
		}
		overlap, err := locationsOverlap(r.Source, r.Destination)
		if err != nil {
			return Command{}, err
		}
		if overlap {
			return Command{}, errors.New("source and destination must not overlap")
		}
		switch r.Type {
		case "copy":
			return Command{"rclone", []string{"copy", r.Source, r.Destination, "--immutable", "--stats", "2s"}}, nil
		case "check":
			return Command{"rclone", []string{"check", r.Source, r.Destination}}, nil
		case "sync-preview":
			return Command{"rclone", []string{"sync", r.Source, r.Destination, "--dry-run", "--stats", "2s"}}, nil
		default:
			if r.Confirm != "DELETE DESTINATION EXTRAS" {
				return Command{}, errors.New("sync deletion requires typed confirmation")
			}
			if err := safeSyncDestination(r.Destination); err != nil {
				return Command{}, err
			}
			return Command{"rclone", []string{"sync", r.Source, r.Destination, "--stats", "2s"}}, nil
		}
	case "restore":
		if err := validateOperand(r.SnapshotID); err != nil {
			return Command{}, errors.New("snapshot ID is required")
		}
		destination, err := restoreDestination(r.Destination)
		if err != nil {
			return Command{}, err
		}
		if r.Confirm != "RESTORE" {
			return Command{}, errors.New("restore requires typed confirmation")
		}
		return Command{"kopia", []string{
			"snapshot", "restore",
			"--no-overwrite-files", "--no-overwrite-directories", "--no-overwrite-symlinks",
			"--write-files-atomically", r.SnapshotID, destination,
		}}, nil
	default:
		return Command{}, fmt.Errorf("unsupported operation %q", r.Type)
	}
}
