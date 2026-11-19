#!/usr/bin/env bash
# Test the first-party HTTP API against synthetic, isolated Kopia data.
set -euo pipefail
umask 077
for tool in go kopia curl python3 openssl; do
  command -v "$tool" >/dev/null || { echo "missing prerequisite: $tool" >&2; exit 1; }
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
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

mkdir -p "$work/source/Documents" "$work/repository" "$work/cache" "$work/logs"
printf 'GoreeCloud Backup synthetic HTTP integration test\n' >"$work/source/Documents/hello.txt"

export KOPIA_CONFIG_PATH="$work/kopia.config"
export KOPIA_PASSWORD='synthetic-private-local-repository-no-production-data'
export KOPIA_CACHE_DIRECTORY="$work/cache"
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false

kopia --disable-file-logging repository create filesystem --path="$work/repository" >/dev/null
go build -o "$work/backupd" "$root/cmd/backupd"
port="$(python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(('127.0.0.1',0))
    print(s.getsockname()[1])
PY
)"
export GCB_LISTEN="127.0.0.1:$port"
export GCB_SOURCE_ROOT="$work/source"
export GCB_API_TOKEN="$(openssl rand -hex 32)"
export GCB_KOPIA_BIN="$(command -v kopia)"
export GCB_RCLONE_BIN="$(command -v rclone || printf '/not/installed/rclone')"

"$work/backupd" >"$work/server.log" 2>&1 &
server_pid="$!"
base="http://127.0.0.1:$port"
ready=""
for i in {1..70}; do
  if curl --fail --silent --max-time 1 "$base/healthz" >/dev/null; then ready=1; break; fi
  sleep .1
done
if [[ -z "$ready" ]]; then echo 'server failed to start' >&2; cat "$work/server.log" >&2; exit 1; fi
auth=(-H "Authorization: Bearer $GCB_API_TOKEN")

unauth_status="$(curl -sS -o /dev/null -w '%{http_code}' "$base/api/v1/status")"
test "$unauth_status" = 401 || { echo "unauthenticated request accepted: $unauth_status" >&2; exit 1; }

curl -fsS --max-time 40 -X POST "${auth[@]}" -H 'Content-Type: application/json' \
  -d '{"source":"Documents"}' "$base/api/v1/snapshots" >"$work/created.json"
curl -fsS --max-time 30 "${auth[@]}" "$base/api/v1/snapshots" >"$work/list.json"
curl -fsS --max-time 45 -X POST "${auth[@]}" "$base/api/v1/verify" >"$work/verify.json"

python3 - "$work/created.json" "$work/list.json" "$work/verify.json" <<'PY'
import json,sys
created,snapshots,verified=(json.load(open(p,encoding='utf-8')) for p in sys.argv[1:])
assert created.get('accepted') is True, created
assert isinstance(snapshots,list) and len(snapshots)>0, snapshots
assert verified.get('verified') is True, verified
assert verified.get('filesRead',0)>=1, verified
assert verified.get('bytesRead',0)>=1, verified
print('PASS: auth rejection, encrypted snapshot create, snapshot JSON and full file verification through HTTP API')
PY
