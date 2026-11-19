#!/usr/bin/env bash
# Synthetic repository corruption drill: never open or mutate existing user data.
set -euo pipefail
umask 077
command -v kopia >/dev/null || { echo 'Kopia CLI required' >&2; exit 1; }
command -v python3 >/dev/null || { echo 'Python3 required' >&2; exit 1; }

work="$(mktemp -d)"
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT
mkdir -p "$work/repo" "$work/source" "$work/cache-good" "$work/logs"
python3 - "$work/source/bulk.bin" <<'PYDATA'
import pathlib,sys
# Unique deterministic bytes are representative and contain no user data.
pathlib.Path(sys.argv[1]).write_bytes((b'GoreeCloud synthetic encrypted-object corruption drill 2026\n'*60000))
PYDATA
export KOPIA_CONFIG_PATH="$work/kopia.config"
export KOPIA_PASSWORD='goreecloud-disposable-corruption-test-credential-only'
export KOPIA_CACHE_DIRECTORY="$work/cache-good"
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false

kopia --disable-file-logging repository create filesystem --path="$work/repo" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/source" >/dev/null 2>&1
kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/healthy.json"
python3 - "$work/healthy.json" <<'PYHEALTHY'
import json,sys
v=json.load(open(sys.argv[1],encoding='utf-8'))
assert v['errorCount']==0 and v['stats']['readFileCount']>=1, v
print('PASS: intact isolated encrypted snapshot verifies before corruption')
PYHEALTHY

# Select the largest non-configuration data pack inside this newly created
# temporary repository only. Do not accept external paths or caller overrides.
python3 - "$work/repo" "$work/pack-path" <<'PYSELECT'
from pathlib import Path
import sys
root=Path(sys.argv[1]).resolve(strict=True)
candidates=[p for p in root.rglob('*.f')
            if p.is_file() and not p.is_symlink()
            and not p.name.startswith(('kopia.','_'))
            and p.stat().st_size>1024]
assert candidates,'No isolated object pack found to corrupt'
pack=max(candidates,key=lambda p:p.stat().st_size)
assert pack.resolve(strict=True).is_relative_to(root)
Path(sys.argv[2]).write_text(str(pack),encoding='utf-8')
PYSELECT
pack="$(cat "$work/pack-path")"
python3 - "$pack" <<'PYCORRUPT'
from pathlib import Path
import sys
path=Path(sys.argv[1])
with path.open('r+b') as item:
    item.seek(128)
    before=item.read(1)
    assert len(before)==1
    item.seek(128)
    item.write(bytes([before[0]^0xff]))
PYCORRUPT

# Ensure verification must fetch encrypted data again instead of merely
# trusting cache entries from the healthy run.
mkdir -p "$work/cache-damaged"
export KOPIA_CACHE_DIRECTORY="$work/cache-damaged"
if kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/damaged.json" 2>"$work/verify-error.log"; then
    python3 - "$work/damaged.json" <<'PYDAMAGED'
import json,sys
try:
    result=json.load(open(sys.argv[1],encoding='utf-8'))
except (ValueError,OSError):
    raise SystemExit('Corruption unexpectedly produced successful verification with no valid error report')
assert result.get('errorCount',0)>0, 'Corruption was not detected'
PYDAMAGED
else
    # An upstream nonzero exit from full-content verification also constitutes
    # detection; no archive is declared healthy after a verification failure.
    :
fi
echo 'PASS: intentionally corrupted encrypted pack was rejected by a fresh integrity check'
