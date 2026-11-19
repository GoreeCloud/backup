# Architecture and data flows

## Components

```text
React web app --same-origin--> Go API <----HTTPS/VPN--- Android native recovery client
                                  | fixed command array, bounded output, timeouts
                                  +-- Kopia CLI -> connected encrypted repository -> storage
                                  +-- Rclone CLI -> configured remote names
```

Kopia creates encrypted snapshots, with deduplication/compression provided by upstream Kopia. GoreeCloud Backup does not implement custom encryption or reinterpret repository objects. The first-party API is primarily a control plane. Its explicitly opt-in Android selected-file ingress endpoint can also receive one 32 MiB-bounded foreground upload over authenticated HTTPS and snapshot it with Kopia; no cloud credentials are transmitted to the browser or Android client. Rclone remote names are exposed only to authorized clients.

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

The API now accepts one required administrator bearer token and an optional independent read-only viewer token (development preview). The viewer token can only access authenticated GET status, snapshot list, configured remote names and redacted capability metadata. Expensive remote provider-capacity queries and all POST mutations remain admin-only. The web application holds the token in volatile component state; Android holds it in volatile Compose state. Neither implements server-based login, per-user identities, account recovery or comprehensive RBAC. This is not yet a GoreeCloud Identity integration.

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

The Kotlin/Jetpack Compose client uses authenticated, system-trusted HTTPS with redirects disabled, strict endpoint validation and a 1 MiB bounded JSON response reader. It reads repository state and up to 100 snapshot summaries and optionally requests full-file integrity verification or a contained server-side restore after explicit user acknowledgement. Rclone discovery failures do not block snapshot browsing. It can access only one explicitly system-picker-selected file at a time for a consented foreground upload, without retaining tokens, adding broad storage permissions, or enabling background jobs. Android UI controls do not replace backend authorization.

## On-demand Rclone provider quota

The API requires an authenticated, single-valued configured Rclone remote name ending in `:`, first verifies that name against the local `listremotes` output, and only then runs `rclone about --json` with fixed arguments. It accepts known nonnegative byte/object-count fields only, bounds runtime/output, and serializes remote inspections per API instance to avoid concurrent provider calls. The browser never polls quotas automatically; unsupported providers return an error instead of fabricated capacity. Storage-provider queries may make network requests or incur charges. No Rclone sync, deletion, file enumeration or remoting configuration operation is exposed by this endpoint.

## Local bind enforcement

The API validates `GCB_LISTEN` as a numeric 127.0.0.1 or IPv6 `::1` address with an explicit nonzero port and refuses wildcard/listen-on-all-interface configurations. External HTTPS ingress is independently provisioned and out of this repository's deployment scope.

Linux contained restoration runs Kopia through an explicit Bubblewrap mount namespace with a read-only host root and the private, newly allocated restore workspace as its sole writable host bind mount; temp/cache/log directories are contained inside that workspace. This is an additional pre-write filesystem boundary beyond the post-write recovered-file audit. It requires the `bwrap` executable. Network access remains available for remote repositories, and this setup does not replace a least-privileged service account or full recovery certification.
The restore capability probe executes a short, read-only Bubblewrap namespace check; a binary existing on disk is insufficient when kernel/AppArmor policy blocks namespace creation. Manual CI loads the Ubuntu-distributed application-specific AppArmor profile on its disposable runner without disabling system-wide user-namespace restrictions. Actual recovery remains gated on independent integration tests.

## Opt-in Android selected-file ingress

Set `GCB_MOBILE_INGEST_ROOT` to an existing, empty, canonical, private (0700) directory **outside and non-overlapping with both `GCB_SOURCE_ROOT` and `GCB_RESTORE_ROOT`** to enable `POST /api/v1/mobile/files`. The endpoint requires the same bearer authentication, a unique UTF-8 leaf filename encoded as unpadded base64url in `X-GCB-Filename-B64` (up to 160 decoded bytes), explicit `X-GCB-Confirm-Upload: true`, and a single `application/octet-stream` media type. It streams at most 32 MiB, serializes with other expensive repository operations, creates an exclusive private staging file, fsyncs, and calls the configured Kopia CLI to create an encrypted snapshot. Plaintext staging is deleted on command success, error or client disconnect. The caller cannot choose a destination path. The server read timeout is two minutes, and long-running snapshot creation uses the existing 30-minute deadline. This is a foreground preview workflow, not a background sync, multi-file ingest queue or full-device backup engine.

The selected-file upload response includes `bytes` and a lowercase `sha256` digest computed from the server-side received stream; the Android client computes the digest during its own streaming send and checks exact equality before displaying success. This is an authenticated receipt, not an independent Kopia restore verification. Sensitive file hashes must not be added to logs or telemetry.

## Browser selected-file flow

The web app reuses the opt-in `POST /api/v1/mobile/files` endpoint for one user-selected document, with a base64url UTF-8 filename header and an explicit consent header. Browser-side validation matches the 32 MiB server ceiling and 160-byte filename limit. The browser streams the `File` object directly and refuses fetch redirects. Tokens and file selections remain in volatile React state; neither is saved in browser storage or transmitted to external API hosts. Remote browser origin must use HTTPS (plain HTTP is accepted only on local loopback). The server creates an encrypted Kopia snapshot but sees plaintext temporarily before encryption; this is not a zero-knowledge upload protocol.


The browser computes SHA-256 of the user-selected file in the secure browser context and checks it against the server's received-bytes receipt before reporting success. This detects accidental upload corruption or mismatched responses but is **not proof of future repository recoverability** or end-to-end encryption.

The opt-in first-party `backupctl verify --accept-provider-costs` command and separate monthly systemd template invoke the same serialized authenticated server-side Kopia 100%-file verification endpoint. The CLI rejects HTTP redirects, errors, malformed or unconfirmed JSON, and missing consent, and does not independently open any repository or cloud provider. No timer or service is installed by default; repeated remote reads can incur provider fees.


The staging root must be deliberately allocated separately from the source-tree snapshot allowlist. On startup a directory listing must be empty; unexpected previous entries force a configuration error rather than risking accidental plaintext inclusion in later snapshots or automatic deletion of unknown files. This is a preview-level defense against directory overlap, not a substitute for disk encryption or external Kopia retention testing.


## Read-only viewer access

When `GCB_VIEW_TOKEN` is configured and distinct from `GCB_API_TOKEN`, the server authenticates single-valued bearer headers and marks matching viewer requests using request-local, unexported context state. It returns `readOnly=true`, disables write/cost capabilities and omits `sourceRoot` from the viewer's GET /api/v1/info response. Any unrecognized token or attempted viewer mutation fails with HTTP 401 and a generic error. Client controls are informational convenience only; authorization is enforced by the Go API on every endpoint. The process launcher removes both secrets from Kopia/Rclone child environments.


## Viewer metadata minimization

The viewer token's authenticated `GET /api/v1/snapshots` response is a strict re-serialized allowlist of snapshot ID, timestamp and a source leaf label. It never forwards the raw Kopia snapshot JSON (which can contain hostnames, usernames, absolute paths, object IDs and tags) to that role. The source label is derived using both Windows and Unix path separators and has control characters removed and an 80-rune limit. Viewer `GET /api/v1/status` returns only connected availability and a generic message. Administrators continue receiving full upstream details. Invalid list JSON fails closed rather than falling back to a raw response; the authenticated viewer continues to see some identifying metadata by design.


## Rclone interoperability acceptance (synthetic local storage)

The manual CI synthetic mirror drill builds both attributed source engines and uses Rclone `copy` and `check` only on unrelated temporary local directories: an encrypted Kopia filesystem repository and a separate mirror. A fresh Kopia connection with its own config and empty cache verifies archived file contents and independently restores a snapshot from the mirror, comparing its bytes and symlinks against synthetic source fixtures. The Rclone config is forcibly empty and independent of host provider settings. The test does **not** imply that a cloud provider is configured, a background sync engine is available, or that a mirror created during concurrent repository writes is transactionally consistent.


The local `backupctl list` client is read-only and limits the response body to 2 MiB. It uses the existing private token file and loopback-only, no-proxy, no-redirect HTTP client. Snapshot records are parsed into an allowlisted struct; stdout shows only sanitized ID, UTC timestamp and folder leaf, never raw upstream host, username, tag, object-root or absolute path data. The optional viewer token is sufficient for this command.

## Android theme parity with GoreeCloud UI

The canonical locally tracked `web/src/gc-tokens.css` token snapshot is parsed by a standard-library Python script to produce Material3 `lightColorScheme` and `darkColorScheme` definitions for native Android. The app follows `isSystemInDarkTheme()` and reads colors from the active Material theme instead of hard-coded light-only values. Build/CI validation rejects any mismatch between generated Kotlin colors and the verified token stylesheet. This is not a claim that all centralized navigation, component, interaction or accessibility requirements are complete.


## Anchored plaintext upload I/O

Selected-file ingestion opens a Go `os.Root` for the canonical, empty, owner-only staging directory and verifies its on-disk identity against the startup check, rather than relying on path validation alone. Secure random subdirectories, exclusive file writes and specific cleanup operations use that handle; if the containing directory is renamed, those I/O operations remain attached to its original inode. A final path identity check precedes Kopia's independent snapshot command. The required Go 1.24 module compatibility is preserved without `os.Root.RemoveAll` (available only in later Go versions). The separate configured source/restore boundary is retained. Review the documented same-UID path lookup race and crash residue limitations before production use.
