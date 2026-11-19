#!/usr/bin/env bash
# Source-built, local-only Rclone SFTP interoperability / Kopia recovery proof.
# Every key, file, repository and server process is synthetic and disposable.
set -euo pipefail
umask 077
for binary in rclone kopia ssh-keygen python3 timeout diff; do
  command -v "$binary" >/dev/null || { echo "missing isolated SFTP prerequisite: $binary" >&2; exit 1; }
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

mkdir -p "$work/source/nested" "$work/repository" "$work/served" "$work/copy-back" \
  "$work/cache-original" "$work/cache-recovered" "$work/kopia-logs" "$work/home" "$work/rclone-cache"
: > "$work/empty-rclone.conf"
chmod 600 "$work/empty-rclone.conf"
rclone_bin="$(command -v rclone)"

# Avoid loading a real developer's Rclone configuration or cloud credentials.
rc() {
  local deadline="$1"
  shift
  timeout "$deadline" env -i HOME="$work/home" PATH="$PATH" "$rclone_bin" "$@" \
    --config "$work/client-rclone.conf" --cache-dir "$work/rclone-cache"
}

python3 - "$work/source" <<'PYDATA'
from pathlib import Path
import hashlib,sys
source=Path(sys.argv[1])
(source/"sample.txt").write_text("GoreeCloud loopback SFTP synthetic encrypted restore\n",encoding="utf-8")
(source/"nested"/"会議-東京.txt").write_text("Unicode synthetic file metadata\n",encoding="utf-8")
with (source/"nested"/"binary.bin").open("wb") as f:
    for i in range(6000):
        f.write(hashlib.sha256(f"disposable sftp recovery source {i}".encode()).digest())
PYDATA
ln -s "sample.txt" "$work/source/relative-link"

export KOPIA_CONFIG_PATH="$work/initial.config"
export KOPIA_PASSWORD="synthetic-sftp-mirror-password-not-for-any-real-repository"
export KOPIA_CACHE_DIRECTORY="$work/cache-original"
export KOPIA_LOG_DIR="$work/kopia-logs"
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false
kopia --disable-file-logging repository create filesystem --path="$work/repository" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/source" >/dev/null 2>&1
kopia --disable-file-logging snapshot list --json >"$work/points.json"
python3 - "$work/points.json" "$work/root-id" <<'PYID'
import json,re,sys
shots=json.load(open(sys.argv[1],encoding="utf-8"))
assert isinstance(shots,list) and len(shots)==1,"expected one synthetic source snapshot"
root=shots[0]["rootEntry"]["obj"]
assert re.fullmatch(r"[A-Za-z0-9_+-]{8,160}",root)
open(sys.argv[2],"w",encoding="utf-8").write(root)
PYID

# Create a one-use synthetic SSH host key and bind only numeric loopback.
ssh-keygen -q -t ed25519 -f "$work/hostkey" -N "" >/dev/null
ssh-keygen -q -t ed25519 -f "$work/wrong-hostkey" -N "" >/dev/null
port="$(python3 - <<'PYPORT'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1",0))
    print(sock.getsockname()[1])
PYPORT
)"
awk -v p="$port" '{print "[127.0.0.1]:" p " " $1 " " $2}' \
  "$work/hostkey.pub" >"$work/known-hosts"
awk -v p="$port" '{print "[127.0.0.1]:" p " " $1 " " $2}' \
  "$work/wrong-hostkey.pub" >"$work/wrong-known-hosts"
chmod 600 "$work/known-hosts" "$work/wrong-known-hosts"

synthetic_user="gcb-sftp-test"
synthetic_pass="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
obscured="$(env -i HOME="$work/home" PATH="$PATH" "$rclone_bin" obscure "$synthetic_pass")"
cat >"$work/client-rclone.conf" <<EOF
[sftp-test]
type = sftp
host = 127.0.0.1
port = $port
user = $synthetic_user
pass = $obscured
known_hosts_file = $work/known-hosts

[sftp-wrong-host]
type = sftp
host = 127.0.0.1
port = $port
user = $synthetic_user
pass = $obscured
known_hosts_file = $work/wrong-known-hosts
EOF
chmod 600 "$work/client-rclone.conf"

env -i HOME="$work/home" PATH="$PATH" "$rclone_bin" serve sftp "$work/served" \
  --config "$work/empty-rclone.conf" --cache-dir "$work/rclone-cache" \
  --addr "127.0.0.1:$port" --user "$synthetic_user" --pass "$synthetic_pass" \
  --key "$work/hostkey" >"$work/server.log" 2>&1 &
server_pid="$!"

ready=""
for _ in $(seq 1 40); do
  if rc 4s lsf "sftp-test:" >/dev/null 2>&1; then ready=1; break; fi
  kill -0 "$server_pid" 2>/dev/null || break
  sleep 0.2
done
if [[ -z "$ready" ]]; then
  echo "FAIL: isolated, authenticated loopback SFTP server was not reachable" >&2
  exit 1
fi

# A wrong pinned host key must fail before transferring repository bytes.
if rc 12s lsf "sftp-wrong-host:" >"$work/wrong-host.log" 2>&1; then
  echo "FAIL: mismatched SSH host key unexpectedly accepted" >&2
  exit 1
fi
grep -Eqi 'host.key|known.host|key.mismatch' "$work/wrong-host.log" || {
  echo "FAIL: wrong-host check failed for an unexpected reason" >&2
  exit 1
}
echo "PASS: synthetic Rclone SFTP client refuses an untrusted host key"

rc 120s copy "$work/repository" "sftp-test:encrypted-repository" \
  --transfers=2 --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/upload.log" 2>&1
rc 90s check "$work/repository" "sftp-test:encrypted-repository" \
  --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/check.log" 2>&1
rc 120s copy "sftp-test:encrypted-repository" "$work/copy-back" \
  --transfers=2 --checkers=2 --retries=1 --low-level-retries=1 \
  >"$work/download.log" 2>&1

# Rclone's SFTP backend may not supply a strong server-side checksum.
# Verify exact encrypted objects independently after copying them back.
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
assert original and original==returned, "encrypted repository object missing or changed in SFTP round trip"
print("PASS: pinned Rclone transferred encrypted Kopia packs over authenticated loopback SFTP byte-for-byte")
PYCOMPARE

# Stop the only SSH server and independently restore the local downloaded copy.
kill "$server_pid" 2>/dev/null || true
wait "$server_pid" 2>/dev/null || true
server_pid=""
export KOPIA_CONFIG_PATH="$work/fresh-restore.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-recovered"
timeout 60s kopia --disable-file-logging repository connect filesystem \
  --path="$work/copy-back" >"$work/reconnect.log" 2>&1
timeout 90s kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json \
  >"$work/verify.json"
python3 - "$work/verify.json" <<'PYVERIFY'
import json,sys
result=json.load(open(sys.argv[1],encoding="utf-8"))
assert result["errorCount"]==0,result
assert result["stats"]["readFileCount"]>=3,result
print("PASS: fresh Kopia connection verified full archived contents after SFTP server shutdown")
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
assert link.is_symlink() and link.readlink()==Path("sample.txt"), "recovered symlink was changed"
PYLINK
echo "PASS: SFTP mirrored synthetic archive independently restored including Unicode data and symlink"
