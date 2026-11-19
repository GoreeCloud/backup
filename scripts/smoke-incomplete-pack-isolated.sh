#!/usr/bin/env bash
# Simulate missing and truncated encrypted object packs in disposable copies.
# This script never opens, deletes, or modifies an existing user's repository.
set -euo pipefail
umask 077

for tool in kopia python3 cp; do
  command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/repo" "$work/source" "$work/logs" "$work/cache-initial"
python3 - "$work/source/synthetic.bin" <<'PYDATA'
from pathlib import Path
import hashlib,sys
with Path(sys.argv[1]).open('wb') as out:
    for n in range(80000):
        out.write(hashlib.sha256(f'GoreeCloud missing-object-transfer test {n}'.encode()).digest())
PYDATA

export KOPIA_PASSWORD='goreecloud-disposable-missing-pack-test-only'
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_CONFIG_PATH="$work/source-repository.config"
export KOPIA_CACHE_DIRECTORY="$work/cache-initial"

kopia --disable-file-logging repository create filesystem --path="$work/repo" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/source" >/dev/null 2>&1
kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/initial-verification.json"
python3 - "$work/initial-verification.json" <<'PYVERIFY'
import json,sys
result=json.load(open(sys.argv[1],encoding='utf-8'))
assert result['errorCount']==0 and result['stats']['readFileCount']>=1,result
print('PASS: baseline synthetic encrypted repository verifies before copying')
PYVERIFY

# Create two clean byte-identical simulated object-store copies. Select the
# largest encrypted pack inside the disposable repository, never an external path.
for case in missing truncated; do
  mkdir -p "$work/mirror-$case"
  cp -a "$work/repo/." "$work/mirror-$case/"
  export KOPIA_CONFIG_PATH="$work/$case-repository.config"
  export KOPIA_CACHE_DIRECTORY="$work/pristine-cache-$case"
  mkdir -p "$KOPIA_CACHE_DIRECTORY"
  kopia --disable-file-logging repository connect filesystem --path="$work/mirror-$case" \
    >"$work/$case-pristine-connect.log" 2>&1
  kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json \
    >"$work/$case-pristine-verification.json" 2>"$work/$case-pristine-log"
  python3 - "$work/$case-pristine-verification.json" <<'PYPRISTINE'
import json,sys
v=json.load(open(sys.argv[1],encoding='utf-8'))
assert v.get('errorCount')==0 and v['stats']['readFileCount']>=1,v
PYPRISTINE
done
echo 'PASS: pristine encrypted repository copies verify before simulated transport damage'

python3 - "$work/mirror-missing" "$work/mirror-truncated" <<'PYDAMAGE'
from pathlib import Path
import sys
for case,rootname in (('missing',sys.argv[1]),('truncated',sys.argv[2])):
    root=Path(rootname).resolve(strict=True)
    packs=[p for p in root.rglob('*.f')
           if p.is_file() and not p.is_symlink()
           and not p.name.startswith(('kopia.','_'))
           and p.stat().st_size>4096]
    assert packs, f'No encrypted data packs in disposable {case} repository'
    pack=max(packs,key=lambda p:p.stat().st_size)
    assert pack.resolve(strict=True).is_relative_to(root),pack
    if case=='missing':
        pack.unlink()
    else:
        original=pack.stat().st_size
        with pack.open('r+b') as item:
            item.truncate(max(1,original//4))
PYDAMAGE

for case in missing truncated; do
  export KOPIA_CONFIG_PATH="$work/$case-repository.config"
  export KOPIA_CACHE_DIRECTORY="$work/cache-$case"
  mkdir -p "$KOPIA_CACHE_DIRECTORY"
  # The pristine copy was already connected and verified; clear the cache
  # after modifying that same copy, forcing a new storage read.
  # An unexpectedly healthy result after data loss must fail CI.
  # Force fresh cache and explicit 100% file-content reads. A healthy success
  # report here would be a serious false negative and must fail CI.
  if kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json \
       >"$work/$case-verification.json" 2>"$work/$case-error.log"; then
    if python3 - "$work/$case-verification.json" <<'PYSTATUS'
import json,sys
result=json.load(open(sys.argv[1],encoding='utf-8'))
# A zero-error exit and report is unexpectedly healthy after data loss.
if result.get('errorCount',0)==0:
    raise SystemExit(0)
raise SystemExit(1)
PYSTATUS
    then
      echo "FAIL: $case encrypted pack passed full-content verification" >&2
      exit 1
    fi
  fi
  echo "PASS: $case encrypted pack detected as unusable in disposable copied repository"
done
