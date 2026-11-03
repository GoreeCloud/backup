#!/usr/bin/env bash
# Destructive operations below are restricted to a newly created test directory.
set -euo pipefail

if ! command -v rclone >/dev/null 2>&1; then
  printf 'SKIP: rclone is not installed\n' >&2
  exit 77
fi

# Snap-packaged Rclone may see a different /tmp namespace. Keep the test in HOME.
test_dir=$(mktemp -d "${HOME:?}/.goreecloud-backup-smoke.XXXXXXXX")
case "$test_dir" in
  "$HOME"/.goreecloud-backup-smoke.*) ;;
  *) printf 'Unsafe test directory; aborting\n' >&2; exit 1 ;;
esac
cleanup() { rm -rf -- "$test_dir"; }
trap cleanup EXIT

mkdir -p "$test_dir/source" "$test_dir/target"
printf 'GoreeCloud Backup disposable transfer test\n' > "$test_dir/source/fixture.txt"

rclone --config /dev/null copy "$test_dir/source" "$test_dir/target" --immutable
rclone --config /dev/null check "$test_dir/source" "$test_dir/target"
cmp "$test_dir/source/fixture.txt" "$test_dir/target/fixture.txt"

printf 'destination-only test content\n' > "$test_dir/target/extra.txt"
rclone --config /dev/null sync "$test_dir/source" "$test_dir/target" --dry-run
# Preview must not delete anything.
test -f "$test_dir/target/extra.txt"

# Actual deletion is restricted to the dedicated disposable target directory.
rclone --config /dev/null sync "$test_dir/source" "$test_dir/target"
test ! -e "$test_dir/target/extra.txt"
rclone --config /dev/null check "$test_dir/source" "$test_dir/target"
cmp "$test_dir/source/fixture.txt" "$test_dir/target/fixture.txt"

printf 'PASS: isolated local Rclone copy, check, preview, sync and content verification\n'
