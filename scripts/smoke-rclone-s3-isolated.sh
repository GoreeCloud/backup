#!/usr/bin/env bash
# Disposable loopback S3 protocol interoperability and offline Kopia recovery.
# Requires pinned source-built Kopia and Rclone. NEVER contacts cloud providers.
set -euo pipefail
umask 077
for executable in rclone kopia timeout python3 diff; do
  command -v "$executable" >/dev/null || {
    echo "Missing source-built test prerequisite: $executable" >&2
    exit 1
  }
done
work="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf -- "$work"
}
trap cleanup EXIT
mkdir -p "$work"/{source/nested,repository,served/gcbtestbucket,copy-back,cache-original,cache-recovered,kopia-logs,home,rclone-cache}
: >"$work/server-rclone.conf"
chmod 600 "$work/server-rclone.conf"
rclone_bin="$(command -v rclone)"
rc() {
  local duration="$1"
  shift
  timeout "$duration" env -i HOME="$work/home" PATH="$PATH" "$rclone_bin" "$@" \
    --config "$work/client-rclone.conf" --cache-dir "$work/rclone-cache"
}

python3 - "$work/source" <<'PYDATA'
from pathlib import Path
import hashlib,sys
root=Path(sys.argv[1])
(root/"notes.txt").write_text("Synthetic S3 transport recovery fixture\n",encoding="utf-8")
(root/"nested"/"Plan-東京.txt").write_text("Disposable Unicode content\n",encoding="utf-8")
with (root/"nested"/"bytes.bin").open("wb") as output:
    for i in range(9000):
        output.write(hashlib.sha256(f"S3 encrypted mirror regression {i}".encode()).digest())
PYDATA
ln -s "notes.txt" "$work/source/relative-link"

export KOPIA_PASSWORD="synthetic-s3-recovery-only-password-not-real"
export KOPIA_CONFIG_PATH="$work/initial.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-original"
export KOPIA_LOG_DIR="$work/kopia-logs"
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false
kopia --disable-file-logging repository create filesystem --path="$work/repository" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/source" >/dev/null 2>&1
kopia --disable-file-logging snapshot list --json >"$work/points.json"
python3 - "$work/points.json" "$work/root-id" <<'PYPOINTS'
import json,re,sys
points=json.load(open(sys.argv[1],encoding="utf-8"))
assert isinstance(points,list) and len(points)==1,"expected one synthetic snapshot"
root=points[0]["rootEntry"]["obj"]
assert re.fullmatch(r"[A-Za-z0-9_+-]{8,160}",root)
open(sys.argv[2],"w",encoding="utf-8").write(root)
PYPOINTS

port="$(python3 - <<'PYPORT'
import socket
with socket.socket() as conn:
    conn.bind(("127.0.0.1",0))
    print(conn.getsockname()[1])
PYPORT
)"
# IDs are purposely NOT AWS-prefixed; this script never contains a live key.
key_id="gcbtestaccess"
key_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
cat >"$work/client-rclone.conf" <<EOF
[s3-test]
type = s3
provider = Rclone
env_auth = false
endpoint = http://127.0.0.1:$port
region = us-east-1
access_key_id = $key_id
secret_access_key = $key_secret
force_path_style = true
no_check_bucket = true

[s3-bad-credential]
type = s3
provider = Rclone
env_auth = false
endpoint = http://127.0.0.1:$port
region = us-east-1
access_key_id = $key_id
secret_access_key = invalid-$key_secret
force_path_style = true
no_check_bucket = true
EOF
chmod 600 "$work/client-rclone.conf"

# Listen exclusively on a disposable numeric-loopback socket.
env -i HOME="$work/home" PATH="$PATH" "$rclone_bin" serve s3 "$work/served" \
  --config "$work/server-rclone.conf" --cache-dir "$work/rclone-cache" \
  --addr "127.0.0.1:$port" --auth-key "$key_id,$key_secret" \
  >"$work/s3-server.log" 2>&1 &
server_pid="$!"

ready=""
for _ in $(seq 1 50); do
  if rc 5s lsf "s3-test:gcbtestbucket" >/dev/null 2>&1; then ready=1; break; fi
  kill -0 "$server_pid" 2>/dev/null || break
  sleep 0.2
done
if [[ -z "$ready" ]]; then
  echo "FAIL: isolated authenticated loopback S3 server not reachable" >&2
  exit 1
fi

# Client must not succeed with invalid SigV4 credentials.
if rc 12s lsf "s3-bad-credential:gcbtestbucket" >"$work/bad-auth.log" 2>&1; then
  echo "FAIL: S3 server accepted an invalid authentication signature" >&2
  exit 1
fi
if ! grep -Eqi '403|accessdenied|signature|invalidaccesskey|Forbidden|Unauthorized' "$work/bad-auth.log"; then
  echo "FAIL: S3 wrong-credential test failed for an unexpected reason" >&2
  exit 1
fi
echo "PASS: isolated S3 SigV4 endpoint rejects invalid client credentials"

# Non-destructive copy/check/download; no rclone sync, purge or delete.
rc 120s copy "$work/repository" "s3-test:gcbtestbucket/encrypted-repository" \
  --transfers=2 --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/upload.log" 2>&1
rc 90s check "$work/repository" "s3-test:gcbtestbucket/encrypted-repository" \
  --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/check.log" 2>&1
rc 120s copy "s3-test:gcbtestbucket/encrypted-repository" "$work/copy-back" \
  --transfers=2 --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/download.log" 2>&1

# Independent SHA-256 check of every encrypted object, not provider ETag alone.
python3 - "$work/repository" "$work/copy-back" <<'PYCOMPARE'
from pathlib import Path
import hashlib,sys
def manifest(root):
    return {
        str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in Path(root).rglob("*") if path.is_file()
    }
original=manifest(sys.argv[1])
returned=manifest(sys.argv[2])
assert original and original==returned,"encrypted objects differ after S3 copy round-trip"
print("PASS: pinned Rclone S3 copy preserves all encrypted Kopia objects byte-for-byte")
PYCOMPARE

kill "$server_pid" 2>/dev/null || true
wait "$server_pid" 2>/dev/null || true
server_pid=""
export KOPIA_CONFIG_PATH="$work/fresh-restore.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-recovered"
timeout 60s kopia --disable-file-logging repository connect filesystem --path="$work/copy-back" \
  >"$work/reconnect.log" 2>&1
timeout 90s kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json \
  >"$work/verify.json"
python3 - "$work/verify.json" <<'PYVERIFY'
import json,sys
status=json.load(open(sys.argv[1],encoding="utf-8"))
assert status["errorCount"]==0,status
assert status["stats"]["readFileCount"]>=3,status
print("PASS: fresh Kopia connection verifies full archive after S3 listener shutdown")
PYVERIFY
root_id="$(cat "$work/root-id")"
timeout 90s kopia --disable-file-logging snapshot restore "$root_id" "$work/recovered" \
  --no-overwrite-files --no-overwrite-directories --no-overwrite-symlinks \
  >"$work/restore.log" 2>&1
diff -r "$work/source" "$work/recovered"
python3 - "$work/recovered" <<'PYLINK'
from pathlib import Path
import sys
link=Path(sys.argv[1])/"relative-link"
assert link.is_symlink() and link.readlink()==Path("notes.txt")
PYLINK
echo "PASS: offline Kopia restore recovers synthetic files and symlink from S3-copied repository"
