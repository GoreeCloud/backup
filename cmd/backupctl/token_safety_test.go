package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateTokenRootAnchoredAcrossParentReplacement(t *testing.T) {
	base := t.TempDir()
	originalParent := filepath.Join(base, "credentials")
	if err := os.Mkdir(originalParent, 0700); err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(originalParent, "token")
	if err := os.WriteFile(originalPath, []byte(exampleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(originalParent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// Replace the absolute directory while the descriptor remains open.
	relocated := filepath.Join(base, "credentials-original")
	if err := os.Rename(originalParent, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(originalParent, 0700); err != nil {
		t.Fatal(err)
	}
	otherToken := strings.Repeat("Q", 32)
	if err := os.WriteFile(originalPath, []byte(otherToken), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readTokenFromRoot(root, "token")
	if err != nil || got != exampleToken {
		t.Fatalf("open root followed substitute credentials directory: %v", err)
	}
	// New invocation is deliberately bound to the currently configured path,
	// which confirms the path was replaced while the earlier held root did not
	// read through it.
	swapped, err := readTokenFile(originalPath)
	if err != nil || swapped != otherToken {
		t.Fatalf("new invocation could not open configured replacement: %v", err)
	}
}

func TestPrivateTokenRootDoesNotFollowSubstitutedParentSymlink(t *testing.T) {
	base := t.TempDir()
	originalParent := filepath.Join(base, "credentials")
	attackerParent := filepath.Join(base, "other")
	if err := os.Mkdir(originalParent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(attackerParent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originalParent, "token"), []byte(exampleToken), 0600); err != nil {
		t.Fatal(err)
	}
	replacement := strings.Repeat("X", 32)
	if err := os.WriteFile(filepath.Join(attackerParent, "token"), []byte(replacement), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(originalParent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(originalParent, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(attackerParent, originalParent); err != nil {
		t.Fatal(err)
	}
	token, err := readTokenFromRoot(root, "token")
	if err != nil || token != exampleToken {
		t.Fatalf("anchored root read from malicious symlink target: %v", err)
	}
	// Relative symlinks replacing the filename must fail rather than cause a
	// second token file to be read from the same directory.
	if err := os.Remove(filepath.Join(base, "old", "token")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(attackerParent, "token"), filepath.Join(base, "old", "token")); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFromRoot(root, "token"); err == nil {
		t.Fatal("accepted substituted token symlink")
	}
}

func TestPrivateTokenRootRejectsInsecureFileMutations(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(string) error
	}{
		{"world-readable", func(p string) error { return os.Chmod(p, 0644) }},
		{"oversized", func(p string) error { return os.WriteFile(p, []byte(strings.Repeat("A", 4097)), 0600) }},
		{"too-short", func(p string) error { return os.WriteFile(p, []byte("short"), 0600) }},
		{"embedded-nul", func(p string) error { return os.WriteFile(p, []byte(strings.Repeat("A", 32)+"\x00"), 0600) }},
		{"deleted", func(p string) error { return os.Remove(p) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			file := filepath.Join(directory, "token")
			if err := os.WriteFile(file, []byte(exampleToken), 0600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := scenario.mutate(file); err != nil {
				t.Fatal(err)
			}
			if _, err := readTokenFromRoot(root, "token"); err == nil {
				t.Fatalf("accepted %s private token modification", scenario.name)
			}
		})
	}
}
