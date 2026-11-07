# Backup integrity verification — development alpha

GoreeCloud Backup provides two **manual, authenticated** Kopia integrity-check jobs for the currently connected repository.

| Operation | Engine command | What it proves |
| --- | --- | --- |
| Verify backup structure | `kopia snapshot verify --verify-files-percent=0 --file-parallelism=2` | Walks snapshot structures and checks referenced repository objects |
| Verify all backup file contents | `kopia snapshot verify --verify-files-percent=100 --file-parallelism=2` | Also downloads, decrypts and decompresses every backed-up file for verification |

**Fresh-cache protection:** GoreeCloud Backup starts each authenticated Kopia
snapshot verification in a newly created mode-0700 temporary cache directory
using Kopia's documented `KOPIA_CACHE_DIRECTORY` override, then removes
the temporary cache on success or failure. This avoids treating intact
previously cached repository content as proof that backing storage is intact.
**The pre-existing Kopia cache is preserved**, including data that might be
important for recovery. Temporary cache creation failure prevents the engine
from starting. Verification now necessarily incurs fresh storage reads; plan
for free temporary disk space, cloud egress fees, and timeouts. The protection
was validated with Kopia 0.23.1 on an isolated local filesystem; other
versions, remote caching layers and providers remain unverified.

The complete check **requires typed confirmation**: `VERIFY ALL BACKUPS`. It is read-oriented and does not intentionally delete or repair repository objects, but it can use significant CPU, disk cache, network bandwidth, and billed cloud egress. The existing 30-minute per-job deadline still applies. Large repositories may need an operator-initiated external Kopia verification process with a longer controlled deadline.

A successful verification is **not** proof that a particular application or operating system can be restored successfully. Continue performing actual test restores and independent checksum comparisons. A failed, cancelled, interrupted or timed-out verification must never be displayed or treated as passed.

Results appear in the job history. The current dashboard displays bounded engine output, not a dedicated verification report; optional private journal persistence records job **status only** and omits filenames and engine output. No unattended recurring verification scheduler, immutable storage policy, automatic corruption repair or production recovery guarantee is claimed.

Official Kopia documentation:
- https://kopia.io/docs/advanced/consistency/
- https://kopia.io/docs/reference/command-line/common/snapshot-verify/

## Opt-in real-engine acceptance

With trusted `kopia` and `rclone` executables available, an operator can run `python3 scripts/smoke-api-e2e.py` from the checked-out source. It creates a **disposable encrypted local repository** and a loopback API, then exercises structural/full verification, file restoration, transfer verification, and safeguarded synchronization. Its source and destination are temporary fixtures; it must never use an existing repository or cloud remote.

The GitHub CI workflow validates script syntax but **does not execute** this live-engine acceptance script. One isolated Linux live-engine run with real Kopia 0.23.1 and Rclone 1.75.2 completed successfully on November 3, 2026; see `docs/TEST_EVIDENCE_2026-11-03.md`. Other platforms, cloud providers, large repositories, cancellation/error paths and production restore readiness remain unverified.


## Cross-product GoreeCloud Git encrypted restore acceptance (November 6, 2026)

The separately maintained [GoreeCloud Git](https://github.com/GoreeCloud/git) project now includes an **explicitly opt-in** integration harness, [`scripts/smoke-backup-api-git-recovery.py`](https://github.com/GoreeCloud/git/blob/main/scripts/smoke-backup-api-git-recovery.py), merged as [Git PR #23](https://github.com/GoreeCloud/git/pull/23). It tests the **actual authenticated loopback GoreeCloud Backup API** against a private **synthetic** bare Git fixture, delegating to an independently verified Kopia executable.

**Verified on an authorized Linux development laptop:** This Backup service was built offline from source commit `944fec373acfbf72112b0dc3f5d5f0a75dbf8e4c` with Go 1.27.3 and no external Go module downloads; `go test ./...` and `go vet ./...` passed. An external official Kopia v0.23.1 Linux x64 release matched its published SHA-256 `416d0f84a3dbb321a8b2d8f0997b1a0a6e915babe79ee76fa6e4d2bd1e1c5178`. The source, compiled service and Kopia executable were mounted **read-only** into a network-isolated Bubblewrap sandbox, with temporary encrypted repository data, credentials and Git objects contained in private scratch space. The separate [Git integration PR #23](https://github.com/GoreeCloud/git/pull/23) and its Python 3.11–3.13 CI passed.

The real API test exercised snapshot creation, **fresh-cache structural and 100% file verification**, snapshot listing, rejection of unauthenticated API requests, missing confirmation and occupied restore destinations, restoring to a new private Git root, Git ref and receive-settings comparison, `git fsck`, native clone comparison and rotation of the restored synthetic web token. It **passed twice**. Neither Backup nor Git changed actual host accounts, production credentials, networking, private data or real connected repositories during these tests.

**Evidence boundary:** The harness is not started by normal CI, does **not** deploy a GoreeCloud Git service, and does **not** prove encrypted **off-host** replication, real disaster recovery or operator key escrow, production scheduling reliability, operational RPO/RTO, host quotas, cross-platform support, or an owner-approved production release. The existing GoreeCloud Backup development alpha and its normal public-source authorization boundaries are unchanged.

## Local encrypted repository-copy recovery acceptance (November 7, 2026)

The opt-in [repository-copy recovery drill](../scripts/smoke-repository-copy-recovery.py)
exercises this Backup API with a **disposable synthetic repository**: create
an encrypted snapshot, verify, make and SHA-256-check a separate local copy,
make the original paths unavailable, reconnect using new Kopia config/cache,
verify again, and restore byte-identical files through a new authenticated
API server. It rejects an invalid repository password and an occupied restore
path. See [recovery scope and limits](REPOSITORY_COPY_RECOVERY.md).
**This is same-host simulation, not proof of real off-host/cloud recovery.**

## Optional post-backup verification from the Linux trigger

The local `goreecloud-backup-trigger` CLI can run `-verify-after=structure`
following a completed snapshot. For `-verify-after=full`, it additionally
requires `-acknowledge-full-verification-cost`. These options are opt-in;
the default `-verify-after=none` does not verify automatically. Full checks
cover the connected repository, not just the latest snapshot. One client wait
deadline applies across the two jobs. The trigger returns a nonzero code if
either job fails, but a completed snapshot is still a distinct result from a
failed subsequent integrity check.

The reproducible isolated Linux acceptance is implemented in
`scripts/smoke-trigger-verify.py`. Refer to the local scheduling guide for
configuration and privacy limits. A successful structural/full check cannot
replace an independent restore exercise.


## Real corruption negative test

The separately run `scripts/smoke-cache-isolation.py` builds a local API
connected to a **new disposable encrypted repository**, verifies it
successfully, deliberately changes a byte in an encrypted content pack
*inside that disposable repository*, and asserts the next full verification
job reports `failed`. It also checks bounded engine output, journal privacy
and cleanup. **Never apply this corruption test to an existing repository.**

On November 3, 2026 the isolated Kopia 0.23.1 Linux negative test passed
with cache isolation enabled; see `docs/TEST_EVIDENCE_2026-11-03.md`.
Kopia's official [caching documentation](https://kopia.io/docs/advanced/caching/)
describes cached repository content and the `KOPIA_CACHE_DIRECTORY`
override. Normal verification is read-only; only the deliberately destructive
**disposable** acceptance script changes repository bytes.
