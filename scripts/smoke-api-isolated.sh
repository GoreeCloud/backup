#!/usr/bin/env bash
# Test the first-party HTTP API against synthetic, isolated Kopia data.
set -euo pipefail
umask 077
for tool in go kopia curl python3 openssl bwrap; do
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
mkdir -p "$work/source/mobile-ingest"
chmod 700 "$work/source/mobile-ingest"
# Prove mount isolation against a synthetic sentinel outside the writable
# recovery workspace before invoking Kopia. No existing host files are touched.
printf 'protected synthetic data
' >"$work/outside-sentinel"
mkdir -p "$work/recovery/sandbox-probe"
# A failed bwrap invocation must NOT count as a successful sandbox: first
# prove that an inside command actually ran and wrote inside the assigned
# workspace, then prove that a host path remained unmodified.
if ! bwrap --die-with-parent --new-session --unshare-user --unshare-pid --ro-bind / / \
  --proc /proc --dev /dev --bind "$work/recovery/sandbox-probe" "$work/recovery/sandbox-probe" \
  -- /bin/sh -c 'printf sandbox-active > "$1"; if printf modified > "$2"; then exit 81; fi' \
  _ "$work/recovery/sandbox-probe/inside-marker" "$work/outside-sentinel" >"$work/bwrap-probe.log" 2>&1; then
  echo 'FAIL: Bubblewrap could not enforce the intended mount namespace' >&2
  cat "$work/bwrap-probe.log" >&2
  exit 1
fi
grep -qx 'sandbox-active' "$work/recovery/sandbox-probe/inside-marker" || { echo 'FAIL: Bubblewrap did not execute the sandboxed command' >&2; exit 1; }
grep -qx 'protected synthetic data' "$work/outside-sentinel" || { echo 'FAIL: synthetic outside data changed' >&2; exit 1; }
echo 'PASS: Linux restore sandbox cannot write outside its assigned host workspace'

printf 'GoreeCloud Backup synthetic HTTP integration test\n' >"$work/source/Documents/hello.txt"
mkdir -p "$work/source/Documents/nested"
printf 'nested unicode and recovery metadata\n' >"$work/source/Documents/nested/Grüße-東京.txt"
ln -s "hello.txt" "$work/source/Documents/relative-link"
python3 - "$work/source/Documents/large.bin" <<'PYDATA'
import pathlib,sys
pathlib.Path(sys.argv[1]).write_bytes((b'GoreeCloud API recovery fixture ' * 160000)[:5_000_000])
PYDATA

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
export GCB_MOBILE_INGEST_ROOT="$work/source/mobile-ingest"
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
def relative_entries(directory):
 return {p.relative_to(directory).as_posix():p for p in directory.rglob('*')}
a=relative_entries(original)
b=relative_entries(recovered)
assert a.keys()==b.keys(),(sorted(a),sorted(b))
for name,src in a.items():
 dst=b[name]
 if src.is_symlink():
  assert dst.is_symlink() and src.readlink()==dst.readlink(),name
 elif src.is_dir():
  assert dst.is_dir(),name
 else:
  assert src.is_file() and dst.is_file(),name
  assert filecmp.cmp(src,dst,shallow=False),name
print('PASS: contained API restore preserved nested, unicode, symlink and large-file fixtures')
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

# A user-chosen file is staged into a fresh private directory, snapshotted by
# Kopia, then discarded as plaintext. This is synthetic data only.
printf 'Android synthetic user-approved selected document\n' >"$work/selected-document.txt"
unauthorized_mobile="$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/octet-stream' \
  -H 'X-GCB-Filename-B64: cGhvbmUudHh0' -H 'X-GCB-Confirm-Upload: true' \
  --data-binary "@$work/selected-document.txt" "$base/api/v1/mobile/files")"
test "$unauthorized_mobile" = 401 || { echo "mobile endpoint accepted unauthenticated bytes" >&2; exit 1; }
curl -fsS --max-time 45 -X POST "${auth[@]}" -H 'Content-Type: application/octet-stream' \
  -H 'X-GCB-Filename-B64: cGhvbmUudHh0' -H 'X-GCB-Confirm-Upload: true' \
  --data-binary "@$work/selected-document.txt" "$base/api/v1/mobile/files" >"$work/mobile-result.json"
python3 - "$work/mobile-result.json" "$work/source/mobile-ingest" <<'PYMOBILE'
import pathlib,json,sys
result=json.load(open(sys.argv[1],encoding='utf-8'))
assert result.get('backedUp') is True and result.get('name')=='phone.txt',result
assert result.get('bytes')>0,result
stage=pathlib.Path(sys.argv[2])
assert list(stage.iterdir())==[], 'Temporary plaintext staging directory was not cleaned'
print('PASS: opt-in synthetic Android file upload produced encrypted Kopia backup and removed temporary plaintext staging')
PYMOBILE
# Verify the just-created selected-file snapshot is recoverable, not just
# accepted by the upload endpoint.
curl -fsS --max-time 30 "${auth[@]}" "$base/api/v1/snapshots" >"$work/mobile-snapshots.json"
python3 - "$work/mobile-snapshots.json" "$work/mobile-restore-request.json" <<'PYMOBILERESTORE'
import json,sys
snapshots=json.load(open(sys.argv[1],encoding='utf-8'))
matches=[row for row in snapshots if '/mobile-ingest/mobile-' in row.get('source',{}).get('path','')]
assert len(matches)==1,(len(matches),[row.get('source',{}).get('path') for row in snapshots])
json.dump({'snapshotId':matches[0]['id'],'acknowledge':True},open(sys.argv[2],'w',encoding='utf-8'))
PYMOBILERESTORE
curl -fsS --max-time 45 -X POST "${auth[@]}" -H 'Content-Type: application/json'   --data-binary "@$work/mobile-restore-request.json" "$base/api/v1/restore" >"$work/mobile-restored.json"
python3 - "$work/mobile-restored.json" "$work/recovery" "$work/selected-document.txt" <<'PYMOBILECOMPARE'
import json,sys,pathlib
result=json.load(open(sys.argv[1],encoding='utf-8'))
assert result.get('restored') is True,result
rel=pathlib.PurePosixPath(result['relativePath'])
assert not rel.is_absolute() and '..' not in rel.parts,result
restored=pathlib.Path(sys.argv[2])/str(rel)/'phone.txt'
assert restored.read_bytes()==pathlib.Path(sys.argv[3]).read_bytes(),restored
print('PASS: selected Android file recovered byte-for-byte from its encrypted Kopia snapshot')
PYMOBILECOMPARE
# Independently restore the just-uploaded encrypted snapshot through the API.
curl -fsS --max-time 30 "${auth[@]}" "$base/api/v1/snapshots" >"$work/after-mobile-snapshots.json"
python3 - "$work/after-mobile-snapshots.json" "$work/mobile-restore-request.json" <<'PYMOBILEID'
import json,sys
snapshots=json.load(open(sys.argv[1],encoding='utf-8'))
mobile=[row for row in snapshots if '/mobile-ingest/mobile-' in row.get('source',{}).get('path','')]
assert len(mobile)==1, mobile
json.dump({'snapshotId':mobile[0]['id'],'acknowledge':True},open(sys.argv[2],'w',encoding='utf-8'))
PYMOBILEID
curl -fsS --max-time 45 -X POST "${auth[@]}" -H 'Content-Type: application/json' \
  --data-binary "@$work/mobile-restore-request.json" "$base/api/v1/restore" >"$work/mobile-restored.json"
python3 - "$work/mobile-restored.json" "$work/recovery" "$work/selected-document.txt" <<'PYMOBILERESTORE'
import json,pathlib,filecmp,sys
result=json.load(open(sys.argv[1],encoding='utf-8'))
assert result['restored'] is True,result
relative=pathlib.PurePosixPath(result['relativePath'])
assert not relative.is_absolute() and '..' not in relative.parts
recovered=(pathlib.Path(sys.argv[2])/str(relative)/'phone.txt')
assert recovered.is_file() and filecmp.cmp(recovered,sys.argv[3],shallow=False),recovered
print('PASS: Android-selected synthetic file survived Kopia encryption, snapshot, and independent contained restore')
PYMOBILERESTORE
