# Security model and limitations

## Current trust boundary

The app listens only on loopback HTTP. It authenticates API access with a random bearer token (at least 24 characters), checks the Origin header on writes when present, applies strict CSP and no-store headers, and uses fixed operations. The UI keeps the token in tab memory, not local storage. There is **no** production-grade multi-user or remote-access security model. The backup host's OS user is the effective storage authorization boundary.

External command execution uses Go `exec.CommandContext`, never a shell. Allowed engines are `kopia` and `rclone`, selected by fixed request types; path operands reject leading option markers and control characters. The worker admits a maximum of 12 pending/running operations, returns HTTP 429 when full, and has a 30-minute limit per running operation. Output is capped to 12 KiB. The management token is excluded from child process environments. Other host environment variables remain inherited and may contain Kopia credentials; protect the host environment and do not enable verbose command logging around secrets.

## Known risks

- A compromised bearer token permits backup execution, copying and potentially deleting destination data via confirmed live sync. The default local-only listener reduces remote exposure but is not a substitute for authorization when the host is shared.
- The management service has the full file and storage permissions of its OS account. Run with least privilege, dedicated directories and credentials. Do not use as a multi-tenant service.
- Live synchronization can delete destination files. It requires one successful same-path dry-run preview completed within 10 minutes and explicit confirmation; the preview is consumed upon acceptance. The output is bounded, and a successful preview is not proof that inputs have not changed afterward. Use isolated destinations, versioning or provider object-lock protection for important data.
- Repository passphrases and storage credentials are not generated, escrowed or recoverable by GoreeCloud Backup. The operator is responsible for secure recovery material.
- Restore checks for a nonexistent destination and atomically creates a private mode-0700 destination immediately before execution. Kopia receives no-overwrite flags and atomic-file write mode. This reduces overwrite races but does not eliminate untrusted symlink-parent risks, filesystem mutations by privileged processes, or partial restores. Do not expose to hostile local users.
- Job state is volatile and output is truncated; successful completion is not verification that restored data is usable.
- No incident alerting, secure external access, persistent **audit** store, authentication rate-limiting, account lockout, storage isolation, token rotation API or automated recovery testing exists yet. The opt-in status journal is not an audit log, and the job admission limit is not an account-level authentication rate limit.

## Deployment gate

The optional `GOREECLOUD_BACKUP_STATE_DIR` creates or opens a mode-0700 private state directory and atomically writes a mode-0600 versioned `jobs.json`. It intentionally excludes credentials, engine output, source/destination file paths and old sync approvals. Failed startup validation rejects symlinked, world/group-readable, corrupt or unsupported state files, and interrupted jobs are marked rather than resumed. No at-rest journal encryption, host-owner verification on all supported platforms, database migration, durable scheduling or process-wide crash recovery is established.\n\nBefore exposure to any network or production environment: separate service accounts, server-side authorisation, per-tenant repositories, durable encrypted job persistence, recovery tests, backup consistency tests, secret management, TLS/reverse-proxy integration, independent security review, metrics, rate-limiting, resource quotas, failure handling and host hardening are required. No production deployment is included in this milestone.
