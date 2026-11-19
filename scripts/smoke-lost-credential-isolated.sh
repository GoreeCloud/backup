#!/usr/bin/env bash
# Verify that a wrong repository passphrase does not unlock a copied encrypted
# backup or damage its subsequent ability to recover with the correct password.
# All repository paths are freshly allocated synthetic data.
set -euo pipefail
umask 077
for tool in kopia python3 cp timeout; do
  command -v "$tool" >/dev/null || { echo "Missing test prerequisite: $tool" >&2; exit 1; }
done
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/repo" "$work/mirror" "$work/source" "$work/logs" "$work/cache-initial"
python3 - "$work/source/test-document.bin" <<'PYDATA'
from pathlib import Path
import hashlib,sys
with Path(sys.argv[1]).open('wb') as f:
    for i in range(20000):
        f.write(hashlib.sha256(f'GoreeCloud disposable credential isolation test {i}'.encode()).digest())
PYDATA

export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_CONFIG_PATH="$work/created.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-initial"
export KOPIA_PASSWORD='correct-synthetic-archive-password-not-for-actual-backups'

kopia --disable-file-logging repository create filesystem --path="$work/repo" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/source" >/dev/null 2>&1
kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/created-verified.json"

# Test a copy of the encrypted repository: no live storage or configured
# personal credentials can be supplied to this script.
cp -a "$work/repo/." "$work/mirror/"
export KOPIA_CONFIG_PATH="$work/mirror-correct.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-mirror-correct"
mkdir -p "$KOPIA_CACHE_DIRECTORY"
kopia --disable-file-logging repository connect filesystem --path="$work/mirror" >"$work/mirror-connect.log" 2>&1
kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/mirror-verified.json"
python3 - "$work/created-verified.json" "$work/mirror-verified.json" <<'PYVERIFY'
import json,sys
for path in sys.argv[1:]:
    result=json.load(open(path,encoding='utf-8'))
    assert result.get('errorCount')==0 and result['stats']['readFileCount']>=1,result
print('PASS: original and copied synthetic encrypted repository verify with correct credentials')
PYVERIFY

# A distinct incorrect passphrase and brand-new config/cache must fail to
# unlock the copied repository. Never prompt or wait indefinitely.
export KOPIA_PASSWORD='intentionally-wrong-synthetic-test-password'
export KOPIA_CONFIG_PATH="$work/mirror-unauthorized.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-unauthorized"
mkdir -p "$KOPIA_CACHE_DIRECTORY"
set +e
timeout 15s kopia --disable-file-logging repository connect filesystem --path="$work/mirror" \
    </dev/null >"$work/wrong-password-stdout.log" 2>"$work/wrong-password-stderr.log"
wrong_status=$?
set -e
if test "$wrong_status" -eq 0;then
  echo 'FAIL: encrypted repository unlocked using an incorrect password' >&2
  exit 1
fi
if test "$wrong_status" -eq 124 || test "$wrong_status" -eq 137;then
  echo 'FAIL: credential attempt timed out rather than safely rejecting access' >&2
  exit 1
fi
echo 'PASS: incorrect passphrase rejected on a fresh copied encrypted repository'

# Returning to a fresh correct connection must still succeed. A failed
# credential attempt must not mutate or render the copied repository unusable.
export KOPIA_PASSWORD='correct-synthetic-archive-password-not-for-actual-backups'
export KOPIA_CONFIG_PATH="$work/mirror-recovered.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-recovered"
mkdir -p "$KOPIA_CACHE_DIRECTORY"
kopia --disable-file-logging repository connect filesystem --path="$work/mirror" >"$work/reconnect.log" 2>&1
kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/recovered-verified.json"
python3 - "$work/recovered-verified.json" <<'PYRECOVER'
import json,sys
v=json.load(open(sys.argv[1],encoding='utf-8'))
assert v.get('errorCount')==0 and v['stats']['readFileCount']>=1,v
print('PASS: correct credential restores encrypted repository verification after rejected login')
PYRECOVER
