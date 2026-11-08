# Encrypted repository-copy recovery acceptance — development alpha

**Scope:** A repeatable, *explicitly opt-in* same-host Linux test of recovery
from a separate copy of the complete encrypted Kopia repository. **This is
not offsite disaster-recovery proof.** Nothing is deployed, installed,
configured in production, or sent to a cloud service.

## Test and dependencies

The [standalone test](../scripts/smoke-repository-copy-recovery.py) requires
Python 3, Go and a separately acquired, inspected and trusted Kopia binary.
`KOPIA_BIN` can identify an absolute path. It creates a fresh mode-0700
`TemporaryDirectory` under the executing user's home directory, with only
synthetic files, a random repository password, local loopback API tokens,
source and copied encrypted repository directories, and private config/cache
files. These credentials exist only during the test and must not be logged or
checked into Git. The script builds the *current checked-out* first-party
Backup API server from source; it does not use a deployed system service.

Example on a trusted Linux development host, after reviewing the test source:

```sh
KOPIA_BIN=/absolute/path/to/independently-verified/kopia \
  python3 scripts/smoke-repository-copy-recovery.py
```

The script intentionally has **no configured cloud remotes**, does not use
Rclone, and does not access any existing repository. It is not run by routine
GitHub CI; CI checks syntax, builds the Go binaries, runs unit/race checks and
verifies the source hash manifest.

## What the test proves

1. Create a new encrypted Kopia filesystem repository with a random password.
2. Start an authenticated loopback GoreeCloud Backup API, back up synthetic
   nested files, and complete structural and *fresh-cache 100%* verification.
3. Copy only the encrypted repository files into a **separate temporary local
   directory** and compare cryptographic SHA-256 digests across all files.
4. Move the original repository, original source and original Kopia config to
   unreachable paths **inside the disposable fixture**; the original paths
   must no longer exist. No real backup or original user data is moved.
5. Confirm that a **wrong repository password** cannot connect to the copy.
6. Connect with a new Kopia config and cache, start a new authenticated API
   process, confirm missing management authentication is denied, and rerun
   structural and full verification against the copied repository.
7. Restore the original snapshot into a new path and compare SHA-256 digests
   for all recovered files with hashes recorded *before* source removal.
   Refuse a second restore over the occupied destination (HTTP 422).
8. Check both API status journals exclude the bearer tokens and source path;
   terminate the test servers and remove the disposable scratch tree.

A successful test demonstrates that **this snapshot can be read through an
independent local repository path and configuration** and that the restore
API produced byte-for-byte equivalent synthetic data. It also exercises
password rejection and the non-overwrite guard. This is a more substantial
recovery check than a successful backup or verification command alone.

## Related GoreeCloud Git encrypted-copy acceptance (November 7, 2026)

The separate [GoreeCloud Git PR #26](https://github.com/GoreeCloud/git/pull/26) merged an **opt-in Git-specific integration test** into the public [Git repository](https://github.com/GoreeCloud/git/blob/main/scripts/smoke-backup-api-git-recovery.py). It reuses the actual authenticated GoreeCloud Backup API and independently verified Kopia CLI, but exercises a **synthetic private bare Git repository** rather than ordinary files. The test first snapshots and restores Git locally, then checks SHA-256 digests of a second *local* encrypted Kopia repository copy. It stops the original API, makes the original synthetic source/repository/config paths unavailable, rejects incorrect Kopia credentials and the first API token, connects with fresh client config and cache, and verifies/restores via a second authenticated Backup instance. Git refs, receive protections, `git fsck`, a native clone and restored web-token rotation are checked.

**Verified development evidence:** The Git test passed with exit code 0 inside an offline Bubblewrap sandbox, using the inspected GoreeCloud Backup source commit `d79b13f0f0494bad61b6be23cb0307acb5c37e67` and a checksum-verified official Kopia v0.23.1 binary. Git [PR #26](https://github.com/GoreeCloud/git/pull/26) and [post-merge Python CI](https://github.com/GoreeCloud/git/actions/runs/42437673784) passed, with **87/87** local Python tests. Independently, this Backup source manifest, offline Go race tests and vet passed. The two repositories' scripts are **separate opt-in evidence**, not unattended backup automation. Neither script proves remote/cloud copy durability, escrow of real credentials, or recovery of production Git data.

## Important limitations and next acceptance gates

- The original and copied repositories are on the **same host** and may reside
  on the same physical storage device. A real host/disk/site disaster could
  destroy both. There is no independently replicated or remotely stored copy.
- The copy uses Python's local filesystem copy routine, **not** an Rclone
  transfer. Thus this test does **not** validate transfer correctness, cloud
  provider credentials/permissions, remote object-store consistency, locking,
  network interruptions, provider fees, or Rclone behavior. The workstation's
  Snap Rclone could not initialize due to a local `snapd` state-lock permission
  error in an earlier attempted variant; this did not affect real user data.
- A disposable password is held in test process memory. Production recovery
  requires independent authorized key escrow and a separately tested restore
  procedure with real operator access. A wrong-key denial is not proof that
  keys are backed up.
- No production RPO/RTO, cross-OS compatibility, retention, scheduled job
  recovery, large repository, application-consistent snapshot or long-term
  repository repair is established.
- A passed copy check does not guarantee durability after unexpected power
  failure, bit rot, provider errors, or an interrupted transfer.

**Release gate:** Before the product is presented as production-ready,
validate a genuinely *off-host encrypted copy*, restore using fresh keys and
configuration in a separate authorized recovery environment, independently
verify restored content and application behavior, and document operator
recovery instructions and the actual failure/recovery evidence.
