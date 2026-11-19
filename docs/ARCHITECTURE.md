# Architecture and data flows

## Components

```text
React web app --same-origin--> Go API <----HTTPS/VPN--- Android native read-only client
                                  | fixed command array, bounded output, timeouts
                                  +-- Kopia CLI -> connected encrypted repository -> storage
                                  +-- Rclone CLI -> configured remote names
```

Kopia creates encrypted snapshots, with deduplication/compression provided by upstream Kopia. GoreeCloud Backup does not implement custom encryption or reinterpret repository objects. The first-party API is a control plane, not a file server. No backup bytes or cloud credentials are transmitted to the browser or Android client. Rclone remote names are exposed only to authorized clients.

## API v1

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/healthz` | Public liveness only; no sensitive state |
| GET | `/api/v1/info` | Authenticated feature and configuration summary |
| GET | `/api/v1/status` | Authenticated Kopia repository status |
| GET | `/api/v1/snapshots` | Authenticated Kopia snapshot JSON |
| POST | `/api/v1/snapshots` | Authenticated `{"source":"relative/subdir"}` -> Kopia snapshot create |
| GET | `/api/v1/remotes` | Authenticated Rclone remote names |
| POST | `/api/v1/verify` | Serialized full-content read/verification of all Kopia snapshot files; provider transfer costs possible |

Single administrator bearer token: *preview only*. The web application holds the token in volatile component state; Android holds it in volatile Compose state. Neither implements server-based login, multi-user policies or a credential recovery facility. This is not a GoreeCloud Identity integration.

## Operations, trust and recovery

The default API listens only on loopback. Kopia and Rclone are OS binaries running under the service account; provision its repository credentials and config independently, grant least-privilege directory access and protect their files from backups/logs. Never run as root. Use a reverse proxy with trusted HTTPS, host validation and network controls for any remote access; rate-limit at the edge. Operators must not advertise the service on the public internet without a hardened access boundary.

The source-allowlist prevents normal traversal and symlink escapes at resolution time. Concurrent directory/symlink changes may still cause TOCTOU issues; isolate backups in a stable, permission-controlled source hierarchy and redesign with filesystem-handle containment before multi-tenant hosting. Snapshot creation and file verification are synchronous with 30-minute timeouts and a single per-process concurrency slot; overlapping requests receive HTTP 409. There is no persistent queue, progress feed or cancellation guarantee if disconnected. The source verifier reads 100% of files and may incur cloud egress fees. The service bearer secret is not inherited by Kopia/Rclone child processes. Contained restoration requires optional `GCB_RESTORE_ROOT` pointing to a separate, existing, non-group/world-writable absolute directory outside and disjoint from the source tree. Clients can select only a snapshot currently listed by Kopia; the service allocates a fresh owner-only workspace and passes Kopia no-overwrite options. No caller-chosen path, direct overwriting of source data or automatic import into a live directory is supported. The operation remains synchronous and may require significant time/disk/network usage. An integration smoke test proves restoration on synthetic data only; production recovery readiness is not established.

The service stores no database; Kopia's own repository and config own durable state. Separate the storage repository from source directories. Back up Kopia connection secrets via a separately secure channel and rehearse offline restoration before operational reliance.

## Opt-in Linux scheduling control

`cmd/backupctl` is a small, first-party loopback HTTP client for invoking the existing authenticated snapshot endpoint from systemd oneshot services. The optional `packaging/systemd/` timer uses systemd credentials to expose a private token *file path*, not an API token in the command or unit environment. The service continues to serialize its own operations. There is no automatically installed timer, extra open port, remote API client, separate data store or unattended action until an operator explicitly provisions and enables it.

## Opt-in Linux scheduling control

`cmd/backupctl` is a small, first-party loopback HTTP client for invoking the existing authenticated snapshot endpoint from systemd oneshot services. The optional `packaging/systemd/` timer uses systemd credentials to expose a private token *file path*, not an API token in the command or unit environment. The service continues to serialize its own operations. There is no automatically installed timer, extra open port, remote API client, separate data store or unattended action until an operator explicitly provisions and enables it.
