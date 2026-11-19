#!/usr/bin/env bash
# Isolated, synthetic Kopia recovery drill. Never reads existing backup configs.
set -euo pipefail
umask 077

command -v kopia >/dev/null || { echo "kopia executable required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 executable required" >&2; exit 1; }

work="$(mktemp -d)"
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT
mkdir -p "$work/data/Documents" "$work/repository" "$work/cache" "$work/logs"
printf 'goreecloud-backup-integration-smoke\n' >"$work/data/Documents/sample.txt"
printf 'with special characters: alpha beta\n' >"$work/data/Documents/second.txt"

export KOPIA_CONFIG_PATH="$work/kopia-config"
export KOPIA_PASSWORD='goreecloud-synthetic-backup-test-password-not-a-real-secret'
export KOPIA_CACHE_DIRECTORY="$work/cache"
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false

# Never reuse an existing repository or a user source. Data and credentials are synthetic.
kopia --disable-file-logging repository create filesystem --path="$work/repository" >/dev/null
kopia --disable-file-logging snapshot create "$work/data/Documents" >/dev/null
kopia --disable-file-logging snapshot list --json >"$work/snapshots.json"
python3 - "$work/snapshots.json" "$work/root-id" <<'PY'
import json, sys
data=json.load(open(sys.argv[1],encoding='utf-8'))
assert isinstance(data,list) and data, "No snapshots created"
item=data[-1]
root=item.get("rootEntry",{})
object_id=root.get("obj") or root.get("objectID") or root.get("id")
if not object_id:
    raise SystemExit("Snapshot listing missing restorable root object ID")
open(sys.argv[2],"w",encoding='utf-8').write(object_id)
PY
root_id="$(cat "$work/root-id")"
kopia --disable-file-logging snapshot restore "$root_id" "$work/recovered" --no-overwrite-files --no-overwrite-directories --no-overwrite-symlinks >/dev/null
diff -r "$work/data/Documents" "$work/recovered"
echo "PASS: Kopia encrypted repository, snapshot, restore and byte-for-byte comparison (synthetic test data)"
