# GoreeCloud Backup

**Status: development alpha — not production-ready, not deployed.**

GoreeCloud Backup is a privacy-first, self-hosted backup and storage control plane. Its intended product scope combines Kopia's encrypted snapshot repositories with Rclone's file-transfer and storage-provider ecosystem under one GoreeCloud interface.

The current implementation is a **working, bounded first milestone**, not the complete feature specification. It provides a local authenticated web command center, a small versioned API, background job execution, job status, Kopia snapshot creation/listing/restoration, and Rclone copy/check/sync-preview/confirmed-sync operations. Kopia and Rclone are **external executables**, not bundled or rebranded forks.

## Current capabilities

| Feature | Implementation |
| --- | --- |
| Private dashboard | Implemented; responsive local web UI, no remote fonts or scripts |
| API authentication | Bearer token, timing-safe comparison, security headers |
| Encrypted backups | Uses externally installed Kopia and its connected repository |
| Snapshot listing | Kopia `snapshot list --json` (raw job output) |
| Backup integrity verification | Kopia snapshot structure check or confirmed full content check; may incur cloud download costs |
| Snapshot creation | Kopia `snapshot create` against a specified local directory |
| Restore | Kopia `snapshot restore` into a **new** destination only |
| Cloud file copy | Rclone `copy --immutable`; existing modified files are not overwritten |
| Data comparison | Rclone `check` |
| Synchronization preview | Rclone `sync --dry-run` |
| Live sync | Rclone `sync`, requires a successful matching preview less than 10 minutes old, one-time preview authorization, typed deletion warning, and rejects root destinations |
| Asynchronous job tracking | One worker, maximum 12 pending/running jobs, 30-minute per-job timeout, latest 100 jobs in memory |
| Engine availability | Reports whether Kopia/Rclone are installed on PATH |
| Opt-in private job status history | Implemented in alpha; at most 100 status records, no paths, engine output or secrets; interrupted jobs marked after restart, not resumed |
| Integrated scheduling/policy API, job resumption, RBAC, multi-tenancy, native apps | **Not yet implemented** |

## Requirements

- Go 1.23 or newer (for building the control plane)
- Linux host for the currently tested source build; other operating systems have **not** been verified
- Kopia installed and separately connected to an encrypted repository for snapshot functionality
- Rclone installed and configured with remotes for remote transfers
- Random management token of at least 24 characters

## Development quick start

```sh
go test -race ./...
go build -o goreecloud-backup ./cmd/goreecloud-backup
export GOREECLOUD_BACKUP_TOKEN="$(openssl rand -hex 32)"
./goreecloud-backup
```

Open `http://127.0.0.1:8787` **on the host running the server** and enter the token. For example, use a separate terminal to create a local Kopia repository first:

```sh
kopia repository create filesystem --path "$HOME/kopia-repository"
kopia repository status
```

Review Kopia's password prompts and key-recovery requirements before using important data. Repository credentials are managed by Kopia, not by this application. Configure Rclone remotes using `rclone config` on the same host when needed. Do not place passwords, tokens, user data, or repository content in a public repository.

**Never publicly expose this HTTP server.** It is explicitly limited to `127.0.0.1`, `localhost`, or `::1`. Remote administration requires a separately audited authenticated transport and authorization architecture; this milestone does not offer one.

## Optional private job status history

The development alpha now supports opt-in, minimal job status persistence across service restarts. The default remains in-memory history. To enable the feature, create an absolute, **private directory owned by the service account**:

```sh
mkdir -p "$HOME/.local/state/goreecloud-backup"
chmod 700 "$HOME/.local/state/goreecloud-backup"
export GOREECLOUD_BACKUP_STATE_DIR="$HOME/.local/state/goreecloud-backup"
```

The service writes `jobs.json` atomically with mode `0600`, checks directory privacy and rejects malformed/unsafe state files on startup. It stores job IDs, types, timestamps, completion states, and generic errors only. **Passwords, access tokens, sources, destinations, file listings, engine output, and sync-preview approvals are never written to the journal.** Incomplete jobs are marked `interrupted` after restart; they are **not** automatically retried or resumed. The state format is versioned, but there is no migration facility yet. If state persistence cannot be established when submitting a new job, the API rejects submission rather than silently accepting it; subsequent write failures are logged and surfaced by the health endpoint.

The feature does not provide a durable scheduler, persistent audit ledger, or enterprise-grade protected database. Back up the metadata file only if the retained status history itself is important.

## API

All `/api/v1` endpoints require `Authorization: Bearer <token>`. State-changing requests additionally require `Content-Type: application/json`. Examples:

```sh
curl -H "Authorization: Bearer $GOREECLOUD_BACKUP_TOKEN" http://127.0.0.1:8787/api/v1/health
curl -H "Authorization: Bearer $GOREECLOUD_BACKUP_TOKEN" http://127.0.0.1:8787/api/v1/jobs
curl -X POST http://127.0.0.1:8787/api/v1/jobs \
  -H "Authorization: Bearer $GOREECLOUD_BACKUP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"type":"backup","source":"/absolute/path/to/important-data"}'
```

The job creation endpoint returns HTTP 202 and a job ID. Query `GET /api/v1/jobs/{id}` or list recent jobs. Operations: `backup`, `snapshots`, `restore`, `copy`, `check`, `sync-preview`, and `sync`. Transfers require `source` and `destination` as absolute local paths or configured Rclone remote paths. Restoration uses the **Kopia root directory ID** of the desired snapshot (not necessarily its manifest ID), `destination` to a path that does not exist, and `confirm` equal to `RESTORE`. Live sync requires `confirm` equal to `DELETE DESTINATION EXTRAS` **and** a successful `sync-preview` for the exact same paths within the last 10 minutes. Previews can authorize only one sync and do not guarantee that files have not changed since the preview.

## Security boundaries and known limitations

Sensitive Bitwarden-compatible and Vaultwarden exports need **encrypted storage and a verified recovery key**, not just restrictive file permissions. Follow the [portable vault export safety gates](docs/PORTABLE_VAULT_EXPORT_SAFETY.md) before placing credential exports on removable media or retiring plaintext copies. These instructions do not imply that the current development alpha encrypts existing files in place or has passed a production recovery gate.

- Only allowlisted subcommands execute. No shell expansion or arbitrary command execution is exposed through the API.
- Do **not** use this development service on a shared untrusted operating-system account. The same Kopia/Rclone host credentials may be accessible to the service and to processes running as its user.
- The API token is filtered out of child processes' environments. Kopia passwords and Rclone configuration remain externally managed; this app does not store them.
- No unattended durable scheduler, resumable job execution, tenant isolation, SSO, native mobile/desktop applications, filesystem-consistent database backup, object-lock configuration, or production deployment is provided yet.
- Operation output can contain sensitive file names and paths; it is shown only to the authenticated local operator and is not persisted by this service.
- Sync can delete destination files. The API enforces a fresh, matching, single-use preview; verify the exact paths and output yourself. Both local and remote storage roots are rejected as sync destinations, including common alternate spellings. A snapshot repository is **not** a sync target.
- Copy uses Rclone `--immutable` in the current build to avoid overwriting destination data.
- Restore refuses pre-existing target directories, reserves a new mode-0700 destination just before running, and passes no-overwrite and atomic-file flags to Kopia. An empty destination can remain after a failed restore; inspect it rather than deleting it blindly.
- In-flight jobs are not resumed after a crash and may be interrupted on process shutdown. If opt-in status history is enabled, old queued/running records are marked interrupted on restart; no job is replayed automatically. At most 12 jobs may be pending/running and further requests receive HTTP 429. Restore testing is mandatory before real reliance.
- The interface was statically reviewed but has not completed browser screenshot, visual, usability, cross-platform, or accessibility acceptance; canonical GC UI integration remains outstanding.
- No third-party source or binaries are included; install Kopia and Rclone separately under their respective licenses.

## Design and architecture

The UI follows GoreeCloud UI's documented principles of calm information hierarchy, purposeful depth, semantic surfaces, and responsive accessibility. **A token/package-level integration with `GoreeCloud/gc-ui` has not yet been verified**, so full design-system compliance is pending. The backend uses Go standard-library components only, an in-memory job queue, a serialized worker, and an explicitly bounded command vocabulary.

See [the full user-defined product feature specification](docs/PRODUCT_SCOPE.md), [implementation roadmap](docs/ROADMAP.md), [security analysis](docs/SECURITY.md), [test evidence](docs/TEST_EVIDENCE_2026-11-03.md), and [third-party attribution](docs/THIRD_PARTY.md).

The opt-in `scripts/smoke-trigger-verify.py` independently exercises sequential snapshot/structural/full verification via the loopback API with a separate trusted Kopia executable and disposable repository. The optional, destructive-test-scoped `scripts/smoke-api-e2e.py` executes real Kopia and Rclone commands through the authenticated local API, using a disposable encrypted repository and no cloud accounts. It requires independently verified binaries and is not part of ordinary CI. The optional `scripts/smoke-local-rclone.sh` replays the local-to-local transfer acceptance against an isolated temporary directory under the invoking user's home directory, with cleanup. It intentionally executes a deletion-capable sync **only inside its own disposable test target**. The companion `scripts/smoke-local-kopia.sh` verifies encrypted repository creation, snapshotting, protected restoration and 100% content verification against disposable files. Inspect either script before running it. The roadmap distinguishes implemented, delegated-to-engine, and planned capabilities.

## Quality evidence

The alpha, including opt-in private job metadata persistence and restart-recovery tests, passed local `go test -race ./...`, `go vet ./...`, and `go build ./cmd/goreecloud-backup`. The 21-file initial publication passed [GitHub Actions Verify](https://github.com/GoreeCloud/backup/actions/runs/41650842846). Rclone 1.75.2 passed isolated local copy/check/sync, and signature- and checksum-verified Kopia 0.23.1 passed encrypted snapshot/restore/full verification. **A real authenticated management API also passed end-to-end Kopia backup/list/restore and Rclone copy/check/sync-preview/confirmed-sync tests** on an authorized Linux host. The published optional runner `scripts/smoke-api-e2e.py` repeated those checks successfully. See [test evidence](docs/TEST_EVIDENCE_2026-11-03.md). Real cloud-provider, durable scheduling, other OS and production security acceptance remain unverified.

**Cross-product Git integration evidence (November 6, 2026):** [GoreeCloud Git PR #23](https://github.com/GoreeCloud/git/pull/23) added an opt-in test of this **actual bearer-authenticated Backup API** with real Kopia encrypted snapshot, fresh-cache full verification and isolated restore of a synthetic private bare Git repository. It passed twice inside an offline disposable sandbox. See [Git restore acceptance evidence](docs/INTEGRITY_VERIFICATION.md). This does **not** establish off-host disaster recovery or production readiness.

## Third-party attribution

Kopia: https://github.com/kopia/kopia | Rclone: https://github.com/rclone/rclone. The project invokes these tools as independent executables; their source is not copied here. A repository license for original GoreeCloud source is **not yet selected**. Public visibility does not grant an implied open-source license.

**Local path safeguards:** local-to-local file operations resolve symlink aliases and reject overlapping source/destination directories, including paths with missing children. The engine rechecks immediately before launch; this does not eliminate filesystem races during a transfer. See `docs/SECURITY.md`.

**Opt-in durable Linux schedule ledger:** A new `goreecloud-backup-scheduler`
one-shot command accepts an owner-only JSON policy file, checks 60-minute to
7-day UTC slots, and records fsynced at-most-once claims in a separate private
status-only state file. Disabled/missed policies are skipped and uncertain
claims after restart are marked interrupted without automatic replay. A
separately reviewed five-minute systemd user-timer example is provided but
**not installed or enabled**. It is not yet the integrated scheduling UI,
policy database, guaranteed-delivery or production recovery system. See
[UTC scheduling and recovery limitations](docs/DURABLE_SCHEDULING.md).

**Scheduler crash/restart drill:** The separately opt-in
`scripts/smoke-scheduler-crash.py` confirms that killing the scheduler after
a fsynced claim does not start a duplicate backup on restart. A real Kopia
job already running under the separate loopback API may complete even while
the schedule ledger correctly reports an uncertain `interrupted` outcome.
This is not automated backup recovery or guaranteed delivery.

**Read-only scheduler status:** `goreecloud-backup-scheduler -show-status
-state-dir=/absolute/private/scheduler-state` reports last UTC slot,
status and API job IDs without reading a policy/token file, connecting to
the API or changing the schedule ledger. A `claimed` status can reflect
either a still-active job or an uncertain interrupted invocation. See the
[UTC scheduling operator guide](docs/DURABLE_SCHEDULING.md).

**Optional Linux scheduling:** the separately built `goreecloud-backup-trigger` client submits a snapshot through the authenticated loopback API and waits for completion. It can optionally follow a successful snapshot with structural verification (`-verify-after=structure`) or, with explicit cloud-cost acknowledgment, full repository verification (`-verify-after=full -acknowledge-full-verification-cost`). The default is **no automatic verification**. Reviewed systemd *examples* provide opt-in timer configuration; nothing is installed or enabled by default. See [local scheduling and safety](docs/LOCAL_SCHEDULING.md).

**Storage-backed verification:** GoreeCloud Backup now runs Kopia structure/full checks with a fresh private temporary cache rather than relying on previously cached content that can mask on-disk corruption. The user's existing Kopia cache is never cleared. A disposable corruption drill is available at `scripts/smoke-cache-isolation.py`. Tests remain local Linux acceptance, not proof of production/cloud resilience.

## Backup integrity verification (development alpha)

Two authenticated Kopia jobs check snapshot structure or verify 100% of stored file contents. Full verification requires the typed phrase `VERIFY ALL BACKUPS`, may download the repository, and is limited by the alpha's 30-minute job timeout. See [integrity-check guidance](docs/INTEGRITY_VERIFICATION.md).

## Operator cancellation (development alpha)

Queued and running operations may be cancelled with authenticated `POST /api/v1/jobs/{id}/cancel` or the dashboard's **Cancel operation** action. Cancellation is best-effort; partial backup/restore/synchronization changes may remain. See [job cancellation and recovery limitations](docs/JOB_CANCELLATION.md). When persistent job history is enabled, an unfinished cancellation is recorded as **interrupted** after restart rather than silently marked successful.
