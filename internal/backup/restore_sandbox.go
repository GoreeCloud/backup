package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Presence of a bwrap binary does not prove it can create a user namespace.
// Ubuntu/AppArmor and container policies may deny the UID mapping. Probe the
// same namespace flags in a read-only, time-bounded child before advertising
// the restore capability to clients.
func restoreSandboxAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	exe, err := exec.LookPath("bwrap")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = run(ctx, exe, "--die-with-parent", "--new-session", "--unshare-user",
		"--unshare-pid", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev",
		"--", "/bin/true")
	return err == nil
}

// runConfinedRestore executes Kopia inside a read-only view of the host
// filesystem with exactly one writable bind: a newly allocated private
// recovery workspace. Network access remains available to cloud repositories.
func runConfinedRestore(ctx context.Context, kopiaBin, objectID, destination, workspace string) error {
	if runtime.GOOS != "linux" {
		return errors.New("contained recovery requires Linux bubblewrap")
	}
	sandboxExe, err := exec.LookPath("bwrap")
	if err != nil {
		return errors.New("contained recovery requires the bubblewrap package")
	}
	for _, dir := range []string{"cache", "tmp", "logs"} {
		if err := os.Mkdir(filepath.Join(workspace, dir), 0700); err != nil {
			return fmt.Errorf("recovery sandbox setup failed: %w", err)
		}
	}
	arguments := restoreSandboxArguments(kopiaBin, objectID, destination, workspace)
	_, err = run(ctx, sandboxExe, arguments...)
	return err
}

// restoreSandboxArguments has no shell evaluation. The source filesystem is
// mounted read-only even if malicious restored symlinks traverse outside the
// writable workspace. A bind mount for the new workspace is intentionally last.
func restoreSandboxArguments(kopiaBin, objectID, destination, workspace string) []string {
	return []string{
		"--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid",
		"--ro-bind", "/", "/",
		"--proc", "/proc", "--dev", "/dev",
		"--bind", workspace, workspace,
		"--chdir", workspace,
		"--setenv", "TMPDIR", filepath.Join(workspace, "tmp"),
		"--setenv", "KOPIA_CACHE_DIRECTORY", filepath.Join(workspace, "cache"),
		"--setenv", "KOPIA_LOG_DIR", filepath.Join(workspace, "logs"),
		"--setenv", "KOPIA_USE_KEYRING", "false",
		"--setenv", "KOPIA_PERSIST_CREDENTIALS_ON_CONNECT", "false",
		"--", kopiaBin, "--disable-file-logging", "snapshot", "restore", objectID, destination,
		"--no-overwrite-files", "--no-overwrite-directories", "--no-overwrite-symlinks",
		"--skip-owners", "--skip-permissions",
	}
}
