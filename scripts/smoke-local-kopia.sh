#!/usr/bin/env bash
# End-to-end encrypted backup and restore only in an isolated temporary repository.
# Requires Kopia CLI, Python 3 and OpenSSL. No system-wide setup or cloud access.
set -euo pipefail

kopia_cmd=${KOPIA_BIN:-kopia}
if ! command -v "$kopia_cmd" >/dev/null 2>&1; then
  printf 'SKIP: Kopia executable is unavailable\n' >&2
  exit 77
fi
for command in python3 openssl; do
  command -v "$command" >/dev/null 2>&1 || { printf 'Missing %s\n' "$command" >&2; exit 77; }
done

# Some Snap-confined tools have a separate /tmp, so this test uses HOME.
work=$(mktemp -d "${HOME:?}/.goreecloud-backup-kopia-smoke.XXXXXXXX")
case "$work" in
  "$HOME"/.goreecloud-backup-kopia-smoke.*) ;;
  *) printf 'Unexpected temporary directory\n' >&2; exit 1 ;;
esac
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT
chmod 700 "$work"
mkdir -p "$work/source/nested" "$work/cache"
printf 'GoreeCloud Backup independent encrypted restore smoke test\n' > "$work/source/important.txt"
printf 'another sample\n' > "$work/source/nested/second.txt"

# This password is random, confined to this process and no real backup data.
export KOPIA_PASSWORD
KOPIA_PASSWORD=$(openssl rand -hex 32)
kopia_args=(--config-file="$work/kopia-config.json")
"$kopia_cmd" "${kopia_args[@]}" repository create filesystem --path="$work/repository" --cache-directory="$work/cache" --no-check-for-updates
"$kopia_cmd" "${kopia_args[@]}" snapshot create "$work/source" --json > "$work/snapshot.json"
root_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["rootEntry"]["obj"])' "$work/snapshot.json")
test -n "$root_id"

# Mirrors the service's private destination reservation and restore flags.
mkdir -m 700 "$work/restored"
"$kopia_cmd" "${kopia_args[@]}" snapshot restore \
  --no-overwrite-files --no-overwrite-directories --no-overwrite-symlinks \
  --write-files-atomically "$root_id" "$work/restored"
diff -qr "$work/source" "$work/restored"
"$kopia_cmd" "${kopia_args[@]}" snapshot verify --verify-files-percent=100 --file-parallelism=2
if grep -R -F -q 'GoreeCloud Backup independent encrypted restore smoke test' "$work/repository"; then
  printf 'FAIL: fixture plaintext detected in repository\n' >&2
  exit 1
fi
printf 'PASS: isolated encrypted Kopia backup, restore, directory comparison and full verification\n'
