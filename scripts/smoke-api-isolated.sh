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

mkdir -p "$work/source/Documents" "$work/repository" "$work/cache" "$work/logs" "$work/recovery"
chmod 700 "$work/recovery"
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
go build -o "$work/backupctl" "$root/cmd/backupctl"
port="$(python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(('127.0.0.1',0))
    print(s.getsockname()[1])
PY
)"
export GCB_LISTEN="127.0.0.1:$port"
export GCB_SOURCE_ROOT="$work/source"
export GCB_RESTORE_ROOT="$work/recovery"
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
# The protected local client uses a private token file; never log or pass a token flag.
printf '%s' "$GCB_API_TOKEN" >"$work/cli-token"
chmod 600 "$work/cli-token"
GCB_API_URL="$base" GCB_API_TOKEN_FILE="$work/cli-token" "$work/backupctl" snapshot --source=Documents >"$work/cli-result.txt"
grep -q 'Kopia snapshot command completed' "$work/cli-result.txt"
echo "PASS: local backupctl authenticated a snapshot request without an argv token"
curl -fsS --max-time 30 "${auth[@]}" "$base/api/v1/snapshots" >"$work/list.json"
curl -fsS --max-time 45 -X POST "${auth[@]}" "$base/api/v1/verify" >"$work/verify.json"
python3 - "$work/list.json" "$work/restore-request.json" <<'PYRESTORE'
import json,sys
data=json.load(open(sys.argv[1],encoding='utf-8'))
assert data and data[-1]['id'],data
json.dump({'snapshotId':data[-1]['id'],'acknowledge':True},open(sys.argv[2],'w',encoding='utf-8'))
PYRESTORE
bad_status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${auth[@]}" -H 'Content-Type: application/json' -d '{"snapshotId":"../bad","acknowledge":true}' "$base/api/v1/restore")"
test "$bad_status" = 400 || { echo "unsafe snapshot identifier accepted: $bad_status" >&2; exit 1; }
curl -fsS --max-time 45 -X POST "${auth[@]}" -H 'Content-Type: application/json'   --data-binary "@$work/restore-request.json" "$base/api/v1/restore" >"$work/restored.json"
python3 - "$work/restored.json" "$work/recovery" "$work/source/Documents" <<'PYCOMPARE'
import json,sys,pathlib,filecmp
result=json.load(open(sys.argv[1],encoding='utf-8'))
assert result.get('restored') is True,result
relative=pathlib.PurePosixPath(result['relativePath'])
assert not relative.is_absolute() and '..' not in relative.parts,result
recovered=(pathlib.Path(sys.argv[2])/str(relative))
original=pathlib.Path(sys.argv[3])
assert recovered.is_dir(),recovered
names=sorted(p.name for p in original.iterdir())
assert names==sorted(p.name for p in recovered.iterdir()),names
for filename in names:
 assert filecmp.cmp(original/filename,recovered/filename,shallow=False),filename
print('PASS: isolated API restore placed verified snapshot files in a fresh private destination')
PYCOMPARE

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
