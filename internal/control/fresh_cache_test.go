package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeKopiaForCacheTest(t *testing.T, exitCode int) string {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ -n \"$GOREECLOUD_BACKUP_TOKEN\" ]; then exit 41; fi\n" +
		"printf '%s' \"$KOPIA_CACHE_DIRECTORY\" > \"$GCB_TEST_CACHE_RECORD\"\n" +
		"if [ -n \"$KOPIA_CACHE_DIRECTORY\" ]; then\n" +
		"  test -d \"$KOPIA_CACHE_DIRECTORY\" || exit 42\n" +
		"  printf 'synthetic encrypted content' > \"$KOPIA_CACHE_DIRECTORY/test-object\"\n" +
		"fi\n" +
		"exit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "kopia"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("GOREECLOUD_BACKUP_TOKEN", "NEVER_EXPOSE_MANAGEMENT_TOKEN")
	return binDir
}

func TestVerificationUsesFreshPrivateCacheAndCleansUp(t *testing.T) {
	fakeKopiaForCacheTest(t, 0)
	stale := t.TempDir()
	record := filepath.Join(t.TempDir(), "cache-location")
	t.Setenv("KOPIA_CACHE_DIRECTORY", stale)
	t.Setenv("GCB_TEST_CACHE_RECORD", record)

	for _, args := range [][]string{
		{"snapshot", "verify", "--verify-files-percent=0", "--file-parallelism=2"},
		{"snapshot", "verify", "--verify-files-percent=100", "--file-parallelism=2"},
	} {
		if _, err := (OSExecutor{}).Run(context.Background(), "kopia", args...); err != nil {
			t.Fatalf("verify command failed: %v", err)
		}
		b, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		path := string(b)
		if path == "" || path == stale || !filepath.IsAbs(path) {
			t.Fatalf("verification used existing cache: %q", path)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary private cache was not removed: %q: %v", path, err)
		}
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("user cache was modified or removed: %v", err)
	}
}

func TestVerificationCacheCleanedWhenKopiaFails(t *testing.T) {
	fakeKopiaForCacheTest(t, 7)
	record := filepath.Join(t.TempDir(), "cache-location")
	t.Setenv("GCB_TEST_CACHE_RECORD", record)
	if _, err := (OSExecutor{}).Run(context.Background(), "kopia",
		"snapshot", "verify", "--verify-files-percent=100"); err == nil {
		t.Fatal("failed engine reported as success")
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(string(data)); !os.IsNotExist(err) {
		t.Fatalf("temporary cache retained after engine failure: %v", err)
	}
}

func TestVerificationCacheFailureDoesNotExecuteEngine(t *testing.T) {
	fakeKopiaForCacheTest(t, 0)
	record := filepath.Join(t.TempDir(), "cache-location")
	t.Setenv("GCB_TEST_CACHE_RECORD", record)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "nonexistent"))
	_, err := (OSExecutor{}).Run(context.Background(), "kopia", "snapshot", "verify")
	if err == nil || !strings.Contains(err.Error(), "cannot create private verification cache") {
		t.Fatalf("cache setup failure not reported: %v", err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("engine was executed when private cache creation failed")
	}
}

func TestOrdinaryKopiaCommandPreservesConfiguredCache(t *testing.T) {
	fakeKopiaForCacheTest(t, 0)
	cache := t.TempDir()
	record := filepath.Join(t.TempDir(), "cache-location")
	t.Setenv("KOPIA_CACHE_DIRECTORY", cache)
	t.Setenv("GCB_TEST_CACHE_RECORD", record)
	if _, err := (OSExecutor{}).Run(context.Background(), "kopia", "snapshot", "list"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != cache {
		t.Fatalf("ordinary command unexpectedly changed cache: %q %v", data, err)
	}
}
