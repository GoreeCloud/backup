package control

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildCommandBackup(t *testing.T) {
	path := t.TempDir()
	got, err := BuildCommand(Request{Type: "backup", Source: path})
	if err != nil {
		t.Fatal(err)
	}
	want := Command{Binary: "kopia", Args: []string{"snapshot", "create", path}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
func TestBuildCommandTransfer(t *testing.T) {
	cases := []struct {
		request Request
		valid   bool
		binary  string
		first   string
	}{
		{Request{Type: "copy", Source: "/tmp/source", Destination: "remote:path"}, true, "rclone", "copy"},
		{Request{Type: "sync-preview", Source: "remote:foo", Destination: "/tmp/output"}, true, "rclone", "sync"},
		{Request{Type: "check", Source: "remote:foo", Destination: "remote:bar"}, true, "rclone", "check"},
		{Request{Type: "sync", Source: "remote:foo", Destination: "remote:bar"}, false, "", ""},
		{Request{Type: "sync", Source: "remote:foo", Destination: "remote:bar", Confirm: "DELETE DESTINATION EXTRAS"}, true, "rclone", "sync"},
		{Request{Type: "sync", Source: "remote:foo", Destination: "remote:", Confirm: "DELETE DESTINATION EXTRAS"}, false, "", ""},
		{Request{Type: "copy", Source: "--danger", Destination: "remote:bar"}, false, "", ""},
		{Request{Type: "copy", Source: "remote:foo", Destination: "remote:foo"}, false, "", ""},
		{Request{Type: "copy", Source: "remote:foo", Destination: "remote:foo/child"}, false, "", ""},
		{Request{Type: "sync-preview", Source: "/tmp/demo", Destination: "/tmp/demo/child"}, false, "", ""},
		{Request{Type: "sync", Source: "remote:folder", Destination: "remote:folder/subfolder", Confirm: "DELETE DESTINATION EXTRAS"}, false, "", ""},
		{Request{Type: "copy", Source: "relative", Destination: "remote:bar"}, false, "", ""},
		{Request{Type: "copy", Source: "remote:foo\nunsafe", Destination: "remote:bar"}, false, "", ""},
	}
	for _, tc := range cases {
		cmd, err := BuildCommand(tc.request)
		if (err == nil) != tc.valid {
			t.Fatalf("request %+v: err=%v", tc.request, err)
		}
		if tc.valid && (cmd.Binary != tc.binary || cmd.Args[0] != tc.first) {
			t.Fatalf("wrong command: %+v", cmd)
		}
	}
}
func TestRestoreRequiresNewDestination(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "restored")
	_, err := BuildCommand(Request{Type: "restore", SnapshotID: "abc", Destination: target, Confirm: "RESTORE"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCommand(Request{Type: "restore", SnapshotID: "abc", Destination: target, Confirm: "RESTORE"}); err == nil {
		t.Fatal("overwrite allowed")
	}
	if _, err := BuildCommand(Request{Type: "restore", SnapshotID: "abc", Destination: filepath.Join(root, "new")}); err == nil {
		t.Fatal("missing confirmation accepted")
	}
}
func TestValidateAddress(t *testing.T) {
	for _, host := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:9000"} {
		if err := ValidateAddress(host); err != nil {
			t.Fatal(host, err)
		}
	}
	for _, host := range []string{"0.0.0.0:8787", ":8787", "192.168.0.2:8787", "https://localhost"} {
		if err := ValidateAddress(host); err == nil {
			t.Fatal("accepted unsafe address", host)
		}
	}
}

func TestSyncRejectsAllStorageRootSpellings(t *testing.T) {
	rootLink := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink("/", rootLink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	for _, destination := range []string{"/", "/tmp/..", "remote:", "remote:/", "remote:////", "remote:/folder/..", "remote:./", "remote:foo/../../", rootLink} {
		_, err := BuildCommand(Request{
			Type: "sync", Source: "/tmp/safe-source", Destination: destination,
			Confirm: "DELETE DESTINATION EXTRAS",
		})
		if err == nil {
			t.Errorf("unsafe destination accepted: %q", destination)
		}
	}
	for _, destination := range []string{"/tmp/safe-destination", "remote:folder", "remote:/folder"} {
		_, err := BuildCommand(Request{
			Type: "sync", Source: "/tmp/safe-source", Destination: destination,
			Confirm: "DELETE DESTINATION EXTRAS",
		})
		if err != nil {
			t.Errorf("safe destination rejected %q: %v", destination, err)
		}
	}
}

func TestRestoreUsesNoOverwriteFlags(t *testing.T) {
	parent := t.TempDir()
	cmd, err := BuildCommand(Request{Type: "restore", SnapshotID: "abc123", Destination: filepath.Join(parent, "restore"), Confirm: "RESTORE"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--no-overwrite-files", "--no-overwrite-directories", "--no-overwrite-symlinks", "--write-files-atomically"} {
		found := false
		for _, arg := range cmd.Args {
			if arg == flag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing restore safety flag %q", flag)
		}
	}
}

func TestRestoreReservesDestinationAtExecution(t *testing.T) {
	engineDir := t.TempDir()
	stub := filepath.Join(engineDir, "kopia")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", engineDir)
	dst := filepath.Join(t.TempDir(), "new-restore")
	cmd, err := BuildCommand(Request{Type: "restore", SnapshotID: "k123", Destination: dst, Confirm: "RESTORE"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (OSExecutor{}).Run(context.Background(), cmd.Binary, cmd.Args...); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dst)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("expected private restore reservation, got %v, %v", info, err)
	}
	if _, err = (OSExecutor{}).Run(context.Background(), cmd.Binary, cmd.Args...); err == nil {
		t.Fatal("restore reused an existing path")
	}
}

func TestRejectsLocalSymlinkAliasOverlap(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	for _, tc := range []struct{ typ, from, to string }{
		{"copy", source, alias},
		{"check", alias, source},
		{"sync-preview", source, filepath.Join(alias, "new-child")},
		{"sync", filepath.Join(alias, "new-child"), source},
		{"copy", source, filepath.Join(alias, "new-child", "nested")},
	} {
		_, err := BuildCommand(Request{Type: tc.typ, Source: tc.from, Destination: tc.to, Confirm: "DELETE DESTINATION EXTRAS"})
		if err == nil {
			t.Errorf("accepted alias overlap: %+v", tc)
		}
	}

	if _, err := BuildCommand(Request{Type: "sync", Source: source, Destination: filepath.Join(base, "unrelated"), Confirm: "DELETE DESTINATION EXTRAS"}); err != nil {
		t.Fatalf("unrelated paths should remain valid: %v", err)
	}
}

func TestRejectsDanglingAndCyclicLocalSymlinks(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(base, "dangling")
	if err := os.Symlink(filepath.Join(base, "missing"), dangling); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	for _, destination := range []string{dangling, filepath.Join(dangling, "child")} {
		if _, err := BuildCommand(Request{Type: "sync", Source: source, Destination: destination, Confirm: "DELETE DESTINATION EXTRAS"}); err == nil {
			t.Errorf("accepted dangling symlink %s", destination)
		}
	}
	cyclic := filepath.Join(base, "cycle")
	if err := os.Symlink(cyclic, cyclic); err != nil {
		t.Skipf("cyclic symlink unsupported: %v", err)
	}
	if _, err := BuildCommand(Request{Type: "sync", Source: source, Destination: cyclic, Confirm: "DELETE DESTINATION EXTRAS"}); err == nil {
		t.Error("accepted cyclic symlink")
	}
}

func TestExecutorRechecksLocalAliasBeforeLaunchingRclone(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "dest")
	command, err := BuildCommand(Request{Type: "copy", Source: source, Destination: target})
	if err != nil {
		t.Fatal(err)
	}
	// This alias is introduced after request validation, while queued.
	if err := os.Symlink(source, target); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if _, err := (OSExecutor{}).Run(context.Background(), command.Binary, command.Args...); err == nil {
		t.Fatal("executor launched rclone despite newly overlapping symlink")
	}
}
