# GoreeCloud Backup — opt-in durable UTC scheduling foundation

**Development alpha only.** This implements a manually configured, Linux-only
one-shot policy runner using a private JSON policy file, an atomic status ledger,
and a user-managed systemd timer. It is a foundation for durable scheduling,
**not** a GUI policy editor, production job database, cross-host scheduler,
notification/alert system, immutable recovery mechanism, or unattended release.

Nothing installs, enables, or deploys the service automatically.

## Safety and semantics

- The policy file is a user-created, absolute-path, **mode-0600 regular file**
  in a private directory. It contains source directory names and therefore
  sensitive metadata. It contains **no** backup passwords or bearer tokens.
- The separate state directory must be mode 0700, and its atomic JSON state
  and invocation lock are mode 0600. State stores **policy IDs, UTC slots, job
  IDs, status and completion timestamps only**, never source paths, engine
  output or credentials. It is not encrypted at rest.
- All policies are **disabled unless** `enabled: true` is explicitly set.
  At most four local snapshot policies are supported per configuration.
- Valid periods are 60–10080 minutes. `offset_minutes` is an alignment offset
  within a period, measured from the Unix epoch in **UTC**. For a daily period
  of 1440 and offset 120, the due window starts at **02:00 UTC every day**;
  no daylight-saving adjustment occurs.
- The runner is eligible only during the **first 10 minutes** of its current
  UTC slot. Older missed slots are **skipped**, not replayed. A five-minute
  external timer supplies repeated invitations to check eligibility.
- A private Linux `flock` serializes concurrent invocations. A durable claim
  is fsynced **before** API job submission. Duplicate or restarted invocations
  do not reissue the same slot. After a crash, a previously claimed slot is
  marked `interrupted`, but **not** automatically retried or resumed. This
  favors avoiding ambiguous duplicate backup operations over guaranteed
  delivery; operators must inspect the actual repository.
- Source directories are checked before a claim and revalidated by the server
  at admission. A missing source fails that invocation and does not consume
  the slot. A failed admitted job is recorded and **not retried in that slot**.
- Policies execute **sequentially** through the existing authenticated
  loopback API. Each policy uses the existing snapshot trigger, optionally
  followed by structural or explicitly acknowledged full verification.
  Full repository checks may consume substantial network/data egress and
  storage resources. Default mode is `none`.
- A client timeout may leave an API job running. A killed scheduler may leave
  an admitted job running even though its claim is later marked interrupted.
  State is only scheduling metadata, **not** a backup or restore proof.

## Installation workflow — manual only

1. Independently validate Kopia/Rclone executables and complete an isolated
   encrypted backup, restore, and integrity test before scheduling anything.
2. Build the scheduler from inspected code:
   `go build -o goreecloud-backup-scheduler ./cmd/goreecloud-backup-scheduler`.
3. Create `~/.config/goreecloud-backup/` with mode 0700. Copy the reviewed
   `packaging/systemd/schedules.json.example` into `schedules.json` using
   mode 0600. Replace the placeholder source directory. Explicitly opt in via
   `enabled: true` only after reviewing the selected backup repository.
4. Keep the API bearer token in a separate private regular `management-token`
   file (mode 0600). Run the management API bound to loopback only.
5. Check the policy *without performing any backup*:
   `goreecloud-backup-scheduler -check-config -config=/absolute/schedules.json`.
6. For a manually authorized test, run:
   `goreecloud-backup-scheduler -run-due -config=/absolute/schedules.json
   -state-dir=/absolute/private/scheduler-state
   -token-file=/absolute/private/management-token`.
   This operation can create backups if a policy is enabled and due.
7. Review and copy the sample service and timer under `packaging/systemd/`
   to `~/.config/systemd/user/`, removing `.example` suffixes and replacing
   the absolute executable placeholder. Check their syntax with
   `systemd-analyze --user verify` when supported.
8. Only when explicitly desired, run `systemctl --user daemon-reload`
   and `systemctl --user enable --now goreecloud-backup-scheduler.timer`.
   Check `systemctl --user list-timers`. Disable with
   `systemctl --user disable --now goreecloud-backup-scheduler.timer`.
   The user manager may stop at logout unless independently configured.

**Do not install both the basic one-source snapshot timer and the new policy
timer for the same source without reviewing duplicate-run effects.**

## Read-only operator status

To inspect the private status-only ledger **without claiming a slot,
submitting a backup, reading the token or modifying state**, run:

```sh
goreecloud-backup-scheduler -show-status \
  -state-dir=/absolute/private/scheduler-state
```

The output is stable/sorted by policy identifier and shows last UTC slot,
`claimed`, `completed`, `failed` or `interrupted` status, and available
snapshot/verification job IDs. No source directory names, engine output or
credentials are read or printed. This requires a real mode-0700 state
directory with a private valid state file; missing, malformed or public
state is **rejected** instead of silently initialized.

Reading a `claimed` entry does **not** change its status: a running
scheduler may own the lock, or a prior invocation may have crashed. Only a
subsequent exclusive `-run-due` invocation reconciles an abandoned
`claimed` entry as `interrupted`. Neither status alone proves an actual
backup succeeded or failed. Consult the authoritative Kopia snapshots and
perform independent test restores before manual recovery.

## Recoverability and current limitations

The ledger's last outcome for each policy can be `claimed` (in flight),
`completed`, `failed`, or `interrupted` (an uncertain prior claim after
restart). It is not a transactional guarantee that a backup did or did not
reach storage. The operator must compare actual Kopia snapshots and perform
independent restores before manually correcting or retrying a problematic
period. An interrupted job is *not* automatically rolled back.

Config edit/replacement during execution, OS clock changes, filesystem races,
external modification of state, remote storage outages, failed persistence,
and forced shutdown have limits beyond this alpha's guarantees. Linux `flock`
and status-only journaling do not establish distributed locking or exactly-once
cloud operations. An empty private ledger is created and fsynced only on true first-run
initialization. If its state file subsequently disappears while its lock
file remains, scheduling **fails closed**; the operator must inspect existing
backups and manually plan recovery. Corrupt, symlinked or public state is
also rejected. Missing/deleted/corrupt private state is not silently
recreated as if it had successfully recorded previous work. The owner must
preserve a backup of private policy and status files and review any recovery.

An opt-in isolated Linux acceptance script is
`scripts/smoke-scheduler.py`. It requires a trusted Kopia executable, builds
test binaries and uses only a disposable encrypted repository and private
loopback service. GitHub CI checks this script's syntax but does **not**
execute live-engine acceptance against cloud storage.


## Real-engine crash/restart acceptance

The separate, opt-in `scripts/smoke-scheduler-crash.py` builds the local API
and scheduler, connects them to a **fresh encrypted disposable Kopia**
filesystem repository, then waits for an actual snapshot job to reach
`running` after the scheduler has fsynced a `claimed` slot. It terminates
**only the scheduler process**, immediately invokes the scheduler again, and
asserts that the same slot is marked `interrupted` without submitting any
second API job. It then waits for the **original** real Kopia job to finish.
This specifically tests that an uncertain schedule record does **not** imply
that the storage operation failed. The script verifies ledger/journal privacy
and removes all temporary fixtures.

The local Linux test passed on November 3, 2026 using the separately
checksum- and signature-verified Kopia 0.23.1 executable. It did **not**
corrupt or delete an existing repository, kill the management API service,
or modify production. It does not prove recovery of a service crash, data
corruption, a power loss, distributed scheduling or application-consistent
backups.
