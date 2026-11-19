#!/usr/bin/env bash
# Synthetic-only interoperability proof: Rclone copies an encrypted Kopia
# repository into a separate local mirror; a fresh Kopia connection verifies
# and restores the copied archive. Never touches real remotes or user data.
set -euo pipefail
umask 077
for tool in kopia rclone python3 diff timeout; do
  command -v "$tool" >/dev/null || { echo "Missing test prerequisite: $tool" >&2; exit 1; }
done
work="$(mktemp -d)"
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT
mkdir -p "$work/data/nested" "$work/repository" "$work/mirror" "$work/logs" "$work/cache-original" "$work/cache-mirror"

# Force an isolated, empty Rclone configuration; never load a user's providers.
export RCLONE_CONFIG="$work/rclone.conf"
: > "$RCLONE_CONFIG"
chmod 600 "$RCLONE_CONFIG"
export RCLONE_NO_CHECK_CERTIFICATE=false
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_CONFIG_PATH="$work/original.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-original"
export KOPIA_PASSWORD='GoreeCloud-Rclone-disposable-mirror-test-password-no-real-accounts'

python3 - "$work/data" <<'PYDATA'
from pathlib import Path
import hashlib,sys
root=Path(sys.argv[1])
(root/"message.txt").write_text("Synthetic encrypted repository mirror comparison\n",encoding="utf-8")
(root/"nested"/"Grüße-東京.txt").write_text("Small Unicode fixture\n",encoding="utf-8")
with (root/"nested"/"binary.dat").open("wb") as f:
    for index in range(10000):
        f.write(hashlib.sha256(f"Rclone encrypted mirror synthetic data {index}".encode()).digest())
PYDATA
ln -s "message.txt" "$work/data/relative-link"
kopia --disable-file-logging repository create filesystem --path="$work/repository" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/data" >/dev/null 2>&1
kopia --disable-file-logging snapshot list --json > "$work/original-snapshots.json"
python3 - "$work/original-snapshots.json" "$work/root-object-id" <<'PYID'
import json,sys,re
data=json.load(open(sys.argv[1],encoding="utf-8"))
assert isinstance(data,list) and len(data)==1,"expected precisely one synthetic snapshot"
obj=data[0]["rootEntry"]["obj"]
assert re.fullmatch(r"[A-Za-z0-9_+-]{8,160}",obj),"unexpected Kopia root object ID"
with open(sys.argv[2],"w",encoding="utf-8") as output:
    output.write(obj)
PYID

# Only a non-destructive Rclone COPY is used. No remote config is available,
# and both paths are distinct and freshly allocated below our private temp root.
test -d "$work/repository" && test -d "$work/mirror"
test "$work/repository" != "$work/mirror"
timeout 120s rclone copy "$work/repository" "$work/mirror" \
  --config "$RCLONE_CONFIG" --transfers=2 --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/rclone-copy.log" 2>&1
timeout 90s rclone check "$work/repository" "$work/mirror" \
  --config "$RCLONE_CONFIG" --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/rclone-check.log" 2>&1

# Prove that at least one encrypted pack was copied, not just empty directories.
python3 - "$work/repository" "$work/mirror" <<'PYMIRROR'
import pathlib,sys
source=pathlib.Path(sys.argv[1])
mirror=pathlib.Path(sys.argv[2])
source_files={str(p.relative_to(source)):p.stat().st_size for p in source.rglob("*") if p.is_file()}
mirror_files={str(p.relative_to(mirror)):p.stat().st_size for p in mirror.rglob("*") if p.is_file()}
assert source_files and source_files==mirror_files,"missing or mismatched mirrored encrypted repository files"
assert sum(source_files.values())>0,"copied an empty encrypted repository"
print("PASS: pinned Rclone copied all synthetic encrypted Kopia repository files without deleting source")
PYMIRROR

# Disconnect by using an unrelated fresh Kopia config and empty cache, then
# authenticate and restore the mirror without reading the original repository.
export KOPIA_CONFIG_PATH="$work/mirror.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-mirror"
timeout 60s kopia --disable-file-logging repository connect filesystem --path="$work/mirror" \
  >"$work/mirror-connect.log" 2>&1
timeout 90s kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json \
  >"$work/mirror-verify.json"
python3 - "$work/mirror-verify.json" <<'PYVERIFY'
import json,sys
result=json.load(open(sys.argv[1],encoding="utf-8"))
assert result.get("errorCount")==0,result
assert result["stats"]["readFileCount"]>=3,result
print("PASS: separate Kopia connection verified 100% of archived files in the Rclone-created mirror")
PYVERIFY
root_id="$(cat "$work/root-object-id")"
timeout 90s kopia --disable-file-logging snapshot restore "$root_id" "$work/recovered" \
  --no-overwrite-files --no-overwrite-directories --no-overwrite-symlinks \
  >"$work/mirror-restore.log" 2>&1
diff -r "$work/data" "$work/recovered"
echo "PASS: Rclone-copied encrypted mirror restored byte-for-byte from a fresh Kopia configuration"
