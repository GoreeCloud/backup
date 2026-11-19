#!/usr/bin/env bash
# Synthetic-only archive metadata regression; no real user files/repositories.
# The hardlink inode relationship is measured and reported, not certified.
set -euo pipefail
umask 077
for tool in kopia python3 diff timeout; do
  command -v "$tool" >/dev/null || { echo "missing metadata test prerequisite: $tool" >&2; exit 1; }
done
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/source/private" "$work/repository" "$work/logs" "$work/cache"
python3 - "$work/source" <<'PYDATA'
from pathlib import Path
import os,sys
root=Path(sys.argv[1])
(root/"secret.txt").write_text("disposable restricted backup data\n",encoding="utf-8")
(root/"execute.sh").write_text("#!/bin/sh\nexit 0\n",encoding="utf-8")
(root/"private"/"会議-メモ.txt").write_text("synthetic Unicode folder fixture\n",encoding="utf-8")
os.chmod(root/"secret.txt",0o600)
os.chmod(root/"execute.sh",0o750)
os.chmod(root/"private",0o700)
os.chmod(root/"private"/"会議-メモ.txt",0o640)
os.utime(root/"secret.txt",(1596240000,1596240000))
os.link(root/"secret.txt",root/"hardlink.txt")
os.symlink("secret.txt",root/"relative-link")
assert os.stat(root/"secret.txt").st_ino == os.stat(root/"hardlink.txt").st_ino
PYDATA
export KOPIA_CONFIG_PATH="$work/kopia.config"
export KOPIA_PASSWORD="synthetic-metadata-acceptance-no-real-credential"
export KOPIA_CACHE_DIRECTORY="$work/cache"
export KOPIA_LOG_DIR="$work/logs"
export KOPIA_USE_KEYRING=false
export KOPIA_CHECK_FOR_UPDATES=false
export KOPIA_PERSIST_CREDENTIALS_ON_CONNECT=false
kopia --disable-file-logging repository create filesystem --path="$work/repository" >/dev/null 2>&1
kopia --disable-file-logging snapshot create "$work/source" >/dev/null 2>&1
kopia --disable-file-logging snapshot verify --verify-files-percent=100 --json >"$work/verify.json"
python3 - "$work/verify.json" <<'PYVERIFY'
import json,sys
result=json.load(open(sys.argv[1],encoding="utf-8"))
assert result.get("errorCount")==0,result
assert result["stats"]["expectedTotalFileCount"]>=4,result
PYVERIFY
kopia --disable-file-logging snapshot list --json >"$work/snapshots.json"
root_id="$(python3 - "$work/snapshots.json" <<'PYID'
import json,re,sys
shots=json.load(open(sys.argv[1],encoding="utf-8"))
assert isinstance(shots,list) and len(shots)==1,"expected a single synthetic snapshot"
obj=shots[0]["rootEntry"]["obj"]
assert re.fullmatch(r"[A-Za-z0-9_+-]{8,160}",obj)
print(obj)
PYID
)"
timeout 90s kopia --disable-file-logging snapshot restore "$root_id" "$work/recovered" \
  --no-overwrite-files --no-overwrite-directories --no-overwrite-symlinks >"$work/restore.log" 2>&1
diff -r "$work/source" "$work/recovered"
python3 - "$work/source" "$work/recovered" <<'PYMETA'
import os,pathlib,stat,sys
source,recovered=(pathlib.Path(arg) for arg in sys.argv[1:])
for relative,expected_mode in (
    ("secret.txt",0o600),
    ("hardlink.txt",0o600),
    ("execute.sh",0o750),
    ("private",0o700),
    ("private/会議-メモ.txt",0o640),
):
    a=source/relative
    b=recovered/relative
    original=stat.S_IMODE(a.stat().st_mode)
    restored=stat.S_IMODE(b.stat().st_mode)
    assert original==expected_mode,(relative,original)
    assert restored==expected_mode,(relative,restored)
for name in ("secret.txt","hardlink.txt","execute.sh","private/会議-メモ.txt"):
    assert (source/name).read_bytes()==(recovered/name).read_bytes(),name
assert abs(os.stat(source/"secret.txt").st_mtime-os.stat(recovered/"secret.txt").st_mtime)<1
assert (recovered/"relative-link").is_symlink()
assert os.readlink(recovered/"relative-link")=="secret.txt"
print("PASS: encrypted synthetic restore retained file/dir permissions, timestamps, symlink target and file bytes")
# Important: recovery can be byte-correct without restoring an inode-sharing
# relationship; record the exact observed behavior rather than claiming it.
original_hardlinked=(source/"secret.txt").stat().st_ino==(source/"hardlink.txt").stat().st_ino
restored_hardlinked=(recovered/"secret.txt").stat().st_ino==(recovered/"hardlink.txt").stat().st_ino
assert original_hardlinked,"source hardlink fixture was not valid"
if restored_hardlinked:
    print("PASS: this Kopia build also retained the hardlink inode relationship")
else:
    print("LIMITATION: hardlinked file contents recovered but the shared inode relationship was not preserved")
PYMETA
