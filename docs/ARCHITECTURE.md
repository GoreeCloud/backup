# Architecture and data flows

## Components

```text
React web app --same-origin--> Go API <----HTTPS/VPN--- Android native recovery client
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

## HTTP boundary hardening

Bearer token and JSON Content-Type headers are deliberately single-valued; ambiguous multiple values are rejected. The bundled web renderer serves only regular files contained inside its canonical web root, rejecting symlinks to external files and avoiding directory listings. Contained restores perform a post-write symlink/special-file audit and fail closed when unsafe files are discovered; the Linux Bubblewrap mount sandbox separately constrains restore writes to a private workspace. See `docs/SECURITY.md` for the residual risk.

## Android recovery companion

The Kotlin/Jetpack Compose client uses authenticated, system-trusted HTTPS with redirects disabled, strict endpoint validation and a 1 MiB bounded JSON response reader. It reads repository state and up to 100 snapshot summaries and optionally requests full-file integrity verification or a contained server-side restore after explicit user acknowledgement. Rclone discovery failures do not block snapshot browsing. It does not access or upload Android device files, retain tokens, add storage permissions, or enable background jobs. Android UI controls do not replace backend authorization.

## On-demand Rclone provider quota

The API requires an authenticated, single-valued configured Rclone remote name ending in `:`, first verifies that name against the local `listremotes` output, and only then runs `rclone about --json` with fixed arguments. It accepts known nonnegative byte/object-count fields only, bounds runtime/output, and serializes remote inspections per API instance to avoid concurrent provider calls. The browser never polls quotas automatically; unsupported providers return an error instead of fabricated capacity. Storage-provider queries may make network requests or incur charges. No Rclone sync, deletion, file enumeration or remoting configuration operation is exposed by this endpoint.

## Local bind enforcement

The API validates `GCB_LISTEN` as a numeric 127.0.0.1 or IPv6 `::1` address with an explicit nonzero port and refuses wildcard/listen-on-all-interface configurations. External HTTPS ingress is independently provisioned and out of this repository's deployment scope.

Linux contained restoration runs Kopia through an explicit Bubblewrap mount namespace with a read-only host root and the private, newly allocated restore workspace as its sole writable host bind mount; temp/cache/log directories are contained inside that workspace. This is an additional pre-write filesystem boundary beyond the post-write recovered-file audit. It requires the `bwrap` executable. Network access remains available for remote repositories, and this setup does not replace a least-privileged service account or full recovery certification.