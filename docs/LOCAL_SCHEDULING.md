# Optional local Linux snapshot timer — development alpha

GoreeCloud Backup has an opt-in Linux **systemd user timer example** and a small
`goreecloud-backup-trigger` CLI. It schedules *backup snapshots* and may
optionally perform an integrity check **after a successful snapshot**. It does
not synchronize, delete, restore or configure Kopia repositories. None of the
units are installed or enabled automatically. This is not a complete durable
job scheduler: there is no calendar editor, recurring-policy API, catch-up
reconciliation, alerting or cross-device scheduling.

## Prerequisites

- Run the GoreeCloud Backup API **on loopback only**, with Kopia already installed
  and connected to a separately initialized encrypted repository.
- Prefer `GOREECLOUD_BACKUP_STATE_DIR` as a private mode-0700 directory for
  metadata-only job history. This does not store source paths or credentials.
- Build locally using `go build -o goreecloud-backup-trigger
  ./cmd/goreecloud-backup-trigger`. Do not publicly expose the HTTP API.
- Create a token file under a private directory, mode 0600, containing **the
  same random token** configured as the main server's
  `GOREECLOUD_BACKUP_TOKEN`. Never paste the token into a unit, shell
  command argument, GitHub commit, or CI log.
- Verify restore behavior and repository retention with disposable data first.

## Manual installation workflow (not executed by the project)

1. Build and inspect the trigger binary and confirm the server is accessible
   only on `127.0.0.1:8787` or another explicitly configured loopback address.
2. Create `~/.config/goreecloud-backup/` with mode 0700 and its
   `management-token` file with mode 0600. A symlinked token file is rejected.
3. Copy the reviewed files from `packaging/systemd/` to
   `~/.config/systemd/user/`, removing the `.example` suffix.
4. Replace `/ABSOLUTE/PATH/goreecloud-backup-trigger` with the verified
   binary path and `/ABSOLUTE/SOURCE` with an absolute non-root directory.
   Avoid paths containing spaces until escaped according to systemd syntax.
5. Manually run the trigger against a disposable source to verify a complete
   backup. It prints job IDs and exits nonzero on failure. To enable a post-backup
   structural check, use `-verify-after=structure`. To check **all stored
   repository file contents**, use `-verify-after=full
   -acknowledge-full-verification-cost`; this is not recommended for routine
   scheduling on large or metered remote repositories.
6. Run `systemd-analyze --user verify` on your finalized unit files when
   supported. Review the timer calendar and randomized delay.
7. Only if desired, explicitly enable the user timer through
   `systemctl --user daemon-reload` and
   `systemctl --user enable --now goreecloud-backup-snapshot.timer`.
   `systemctl --user list-timers` shows its schedule. To disable it,
   use `systemctl --user disable --now goreecloud-backup-snapshot.timer`.

## Optional post-backup verification

- Default `-verify-after=none`: submit a snapshot and report its final status.
- `-verify-after=structure`: after **successful** snapshot completion, submit
  `verify-structure` using the same authenticated local management API.
- `-verify-after=full -acknowledge-full-verification-cost`: after successful
  snapshot completion, submit `verify-full` with the required API confirmation
  `VERIFY ALL BACKUPS`. This verifies **the connected repository**, not only
  the newly created snapshot. It can consume substantial time, CPU, bandwidth,
  and billed cloud egress.
- The explicit cost acknowledgment is rejected unless full mode is selected;
  specifying full mode without the acknowledgment is rejected **before backup
  submission**. Invalid mode names are rejected before submission too.
- A failed, cancelled, interrupted, or timed-out backup prevents starting a
  verification job. If verification fails, the CLI exits nonzero while
  preserving the fact that the snapshot job separately succeeded.
- `-max-wait` is one **shared** deadline for both operations (default 35m,
  maximum 45m). The API imposes an independent 30m limit on each job. If
  waiting times out, the server job **may continue to run**. These checks do
  not establish a successful test restore or point-in-time application
  consistency.

Example (manual, not executed by the examples):

```sh
goreecloud-backup-trigger -source=/home/user/Documents \
  -token-file=/home/user/.config/goreecloud-backup/management-token \
  -verify-after=structure
```

**Independent disposable acceptance:** With an independently trusted Kopia
binary in `KOPIA_BIN`, run `python3 scripts/smoke-trigger-verify.py`. It creates
its own encrypted repository and loopback server under a private temporary
directory, verifies both optional modes, checks that unacknowledged full mode
submits no API jobs, confirms metadata-only journal privacy, and cleans up.
GitHub CI checks script syntax; it does **not** run this live-engine test.

**Missed run policy:** `Persistent=false` intentionally skips missed timer
events rather than launching an unexpected backup after downtime. A user
systemd manager may not remain active after logout unless separately configured;
that configuration is not managed by GoreeCloud Backup.

## Security and operational limits

- The trigger reads only a private regular token file, checks loopback targets,
  blocks HTTP redirects, and submits only a validated local directory backup.
- It waits for every requested API job to finish. A failed, cancelled, or
  interrupted backup or post-backup integrity check produces a nonzero exit code. Network or wait timeout also reports failure;
  the server operation **may still be running** after the client exits.
- The server's 30-minute job limit is independent of the trigger's configurable
  35-minute default wait. The timer example allows up to 45 minutes.
- Never treat a submitted job as a verified backup: perform independent restore
  checks and keep offsite or immutable copies.
- Unit arguments reveal the configured source directory to local process-list
  viewers. Token file location is not a credential, but its contents must stay
  private. This example is intended for a trusted single-user local host.
- No signed Linux package, installed service, full scheduling UI, guaranteed
  missed-run delivery or durable multi-host recovery is claimed.
