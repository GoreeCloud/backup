# Opt-in Linux systemd backup scheduling

**These are source templates only. No OS units, schedules, credentials or services are installed or enabled automatically.** Review both units and test a restore before relying on scheduled backups. This workflow calls the existing, local-only API; it does not bypass its authentication or source allowlist.

## Prerequisites

- A configured, running GoreeCloud Backup service bound to `127.0.0.1:8787`.
- A separately provisioned least-privilege service account `goreecloud-backup`, and a Kopia repository connected under the API service user's environment.
- An API bearer credential generated securely (minimum 32 bytes). Store it *outside* Git at `/etc/goreecloud/backup/api-token` and restrict it to root (0600 or stricter). The unit uses systemd `LoadCredential` to pass a private copy into the invocation, never a shell argument or a public environment variable.
- Modern systemd supporting `LoadCredential` and `%d` credential-directory specifier. Review actual service account, local endpoint and path before installation.

## Build (read only until you intentionally install)

```bash
go build -trimpath -o build/bin/goreecloud-backupctl ./cmd/backupctl
go test -race ./cmd/backupctl
```

After reviewing the built source and environment, an administrator may install `build/bin/goreecloud-backupctl` to `/usr/local/bin/goreecloud-backupctl`, and these units into `/etc/systemd/system/`. No automated installer is provided. Provision the credential securely and verify its permissions. Do not copy real tokens into issue reports, shell history, public files or chat.

An operator can explicitly enable one approved source directory:

```bash
# Source is an allowlisted directory named "Documents" below GCB_SOURCE_ROOT.
sudo systemctl daemon-reload
sudo systemctl enable --now 'goreecloud-backup@Documents.timer'
sudo systemctl list-timers 'goreecloud-backup@*'
```

The timer waits until the next configured 03:00 local-time daily window, with up to 30 minutes of randomized delay; `Persistent=true` attempts a catch-up after a missed scheduled interval. **No snapshot is created merely by committing these files.**

For more than one schedule, enable individual timer instances for explicit source subdirectories. If the local API uses a different loopback port, edit the reviewed unit template to match. For a manual test *after* connecting an isolated test repository and source, an operator can start the corresponding service instance and inspect `systemctl status`/journal results. Test recoverability independently.

## Limitations

The timer is a development-stage single-device scheduling facility: no scheduler UI, persistent job queue, backup success proofs, conflict controls across multiple server instances or automatic cloud restore tests. The API serializes its own snapshot, integrity and restore jobs. Scheduling a source that mutates during backup (such as a database) does not guarantee application-consistent recovery.

The client deliberately accepts **only numeric loopback HTTP** and a protected on-disk credential file; it refuses remote/custom hosts and redirects. The client reports generic HTTP statuses and never prints the token. A timer can fail safely when the server is unavailable. Audit journal retention and service-account permissions appropriate to the host's privacy policy.

To stop scheduled snapshots, the operator can disable the timer instance with systemd. Do not delete Kopia repository data merely to disable scheduling.


## Optional full-archive integrity verification (separate consent)

`backupctl verify --accept-provider-costs` invokes the existing authenticated, serialized `POST /api/v1/verify` API and refuses to run without the explicit cost-acknowledgement flag. The server asks Kopia to read 100% of archived file contents. This may cause **substantial remote-provider download/egress charges**, particularly for large backups. The client verifies a bounded JSON result and requires a positive number of files read and an explicit success indication; it never treats HTTP errors, redirects, malformed JSON, or zero verified files as success. Even a passing integrity check does not prove file restore correctness.

A separate `goreecloud-backup-verify.service` and `goreecloud-backup-verify.timer` template is supplied. Both are **disabled and uninstalled by default**. The timer's monthly window starts on the first day of each month at 04:00 local server time, with up to three hours of randomized delay; `Persistent=true` may run a missed check after the host restarts. The service explicitly includes `--accept-provider-costs`, so **installing and enabling this unit is an affirmative operator decision to allow repeated provider reads**. Review cloud billing, source/capacity, retention and test results before enabling it. Do not enable this on unmetered assumptions. To stop checks, disable the timer. This template neither changes existing backup schedules nor grants remote server access.
