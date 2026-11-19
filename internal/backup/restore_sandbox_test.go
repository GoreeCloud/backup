package backup

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSandboxRestorationArguments(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "restore-private")
	dest := filepath.Join(workspace, "files")
	args := restoreSandboxArguments("/usr/local/bin/kopia", "abcd1234", dest, workspace)
	tokens := strings.Join(args, " ")
	mandatory := []string{
		"--ro-bind / /", "--bind " + workspace + " " + workspace,
		"--die-with-parent", "--unshare-user", "--unshare-pid",
		"--setenv KOPIA_CACHE_DIRECTORY " + filepath.Join(workspace, "cache"),
		"--setenv TMPDIR " + filepath.Join(workspace, "tmp"),
		"--no-overwrite-files", "--skip-owners", "--skip-permissions",
	}
	for _, value := range mandatory {
		if !strings.Contains(tokens, value) {
			t.Errorf("missing required confinement option %q", value)
		}
	}
	// Only the newly created workspace is writable: root and CLI config are
	// deliberately not added as writable binds.
	if strings.Count(tokens, "--bind ") != 1 {
		t.Fatal("unexpected additional writable mounts")
	}
	if !strings.Contains(tokens, "-- /usr/local/bin/kopia --disable-file-logging snapshot restore abcd1234 "+dest) {
		t.Fatal("restore arguments have unexpected ordering")
	}
}
func TestSandboxMissingFailsClosed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only sandbox")
	}
	t.Setenv("PATH", t.TempDir())
	work := t.TempDir()
	err := runConfinedRestore(context.Background(), "/usr/bin/kopia", "abcd1234", filepath.Join(work, "files"), work)
	if err == nil || !strings.Contains(err.Error(), "bubblewrap") {
		t.Fatalf("sandbox missing accepted: %v", err)
	}
	if restoreSandboxAvailable() {
		t.Fatal("reported enabled sandbox with no executable")
	}
}
