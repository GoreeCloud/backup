# GoreeCloud Backup — Feature Inventory

| Capability | Current state | Verification boundary |
| --- | --- | --- |
| Kopia repository status | Implemented in Go API | Requires an installed, connected Kopia CLI; no real repository accepted |
| Encrypted snapshot creation | Implemented via Kopia CLI | Only under configured existing source root; synthetic restore passed, real recovery pending |
| Corrupted encrypted-pack detection | Synthetic isolated test and manual CI workflow implemented | Single-byte encrypted pack tampering caught; no cloud/large-scale disaster certification |
| Full 100% archived-file verification | Implemented as authenticated, serialized, user-confirmed API action | Synthetic CLI and HTTP API tests pass; independent restore remains necessary |
| Snapshot listing | Implemented via Kopia JSON CLI | Payload compatibility and pagination need integration tests |
| Pinned Kopia and Rclone upstream source | Included under `third_party/`; file hashes checked | Full transitive license/dependency audit and release packaging pending |
| Opt-in Rclone remote capacity reporting | Implemented for supported, already configured remotes; explicit browser action | Isolated stub/authorization tests pass; live provider compatibility unverified |
| Rclone remote discovery | Implemented in Go API | Names only; no upload/sync operation |
| Web app | React/TypeScript source | Browser and accessibility acceptance not yet complete |
| Android app | Kotlin/Jetpack Compose recovery companion source | HTTPS snapshot browser, consent-based integrity verification and contained restore requests; CI debug APK available for testing; physical-device and signed-release acceptance pending |
| Rclone backend as Kopia storage | Upstream CLI setup only | Experimental/not maintained upstream; not validated here |
| Contained restore via API and web | Implemented, disabled unless `GCB_RESTORE_ROOT` is configured | An isolated synthetic HTTP restore passed including nested paths/symlinks/Unicode/large files; post-write unsafe-symlink/special-file audit and Linux Bubblewrap read-only-host confinement; production recovery acceptance is pending |
| Advanced recovery browsing, pruning, retention UI | Planned | Requires safe restore-target handling and nonproduction drills |
| Opt-in Linux full-content verification CLI and monthly timer template | Implemented; explicit provider-cost flag and operator installation required | Local synthetic API integration, Go race unit tests and systemd syntax check passed; no timer enabled |
| Opt-in Linux systemd scheduled snapshots | Source templates and protected local CLI implemented | Explicit operator installation required; not active or deployed |
| Cross-platform schedule UI, alerts and persistent job management | Planned | Needs durable scheduler, state tracking and secure notifications |
| Android one-file foreground backup | Implemented as opt-in source staging + Kopia snapshot and system-picker client | Synthetic HTTP upload, SHA-256 receipt, encrypt and staging cleanup tested; physical-device/upload TLS acceptance pending |
| Bulk/automatic Android device backup and device restore | Planned | Scoped storage/permissions, policy, durable jobs and recovery drills required |
| GoreeCloud Mesh, Identity, GoreeShield, Wardveil integration | Requirements identified | No external runtime integration claimed |

Source and tests establish development capabilities, not production backup reliability.


**Supply-chain visibility:** Manually invoked Recovery Integration CI also generates a dated-by-workflow resolved-Go-module CycloneDX inventory artifact for the attributed Kopia and Rclone source trees. This records module selection only; full transitive licensing/advisory and release provenance audits remain open.

**Web selected-file backup:** The React workspace now offers the same consent-gated, single-file foreground upload as the Android system-picker client when the private server ingress is configured. Source selection remains explicit; automatic browser directory access, background synchronization and client-side end-to-end encryption are not claimed.

Synthetic resilience coverage: source-built Kopia encrypted-pack tampering, missing-pack mirror, truncated-pack mirror, duplicate snapshots, Unicode paths, symlinks and byte-for-byte contained restoration. Real provider interruption and live disaster-recovery validation remain outstanding.


Credential resilience test: a wrong passphrase on a clean copied encrypted repository must be rejected without hanging, and a fresh correct passphrase must still unlock and verify the archive. This is not lost-key recovery.

**Private ingestion boundary:** New Android and browser selected-file backup setups must keep temporary plaintext under a dedicated, empty private staging root outside normal backup source and restore trees. Startup rejects crash leftovers for operator review instead of automatically erasing them. Existing in-source preview configurations require manual migration before the endpoint can be enabled.


**Access scope:** Optional read-only `GCB_VIEW_TOKEN` for status, snapshot metadata, configured Rclone remote names and redacted capability details. Administrative token remains required for writes, restores, uploads, full-file verification and remote capacity inspection. This is only a coarse shared-token preview, not GoreeCloud Identity or production RBAC.


**Upload failure regression:** The selected-file endpoint is tested against premature EOF, mismatched declared length, stream sizes above 32 MiB without a Content-Length and zero-byte content. Handled errors must reject the snapshot before Kopia is invoked and leave no plaintext staging residue; crash-recovery and resumable transfer are not implemented.


**Viewer privacy:** Read-only API status uses a generic connected message and snapshot responses expose only snapshot ID, timestamp and a bounded source-folder leaf. Full host/user/source paths, root object IDs and tags remain available only through the administrator token. This does not provide anonymous or per-user access control.


**Rclone interoperability regression:** Source-built Rclone local `copy` and `check` of synthetic encrypted Kopia repository data, followed by a new Kopia connection, full-content verification and byte-for-byte recovery from the mirror. Real S3/SFTP/NAS/cloud backends and repository-consistent live mirroring remain unverified; no remote transfer API is enabled.


**Local snapshot inventory CLI:** `backupctl list` retrieves up to 100 recent snapshot IDs and sanitized source leaf labels using a private admin or read-only viewer token and strict loopback-only transport. Raw upstream paths, host/user identities, root object IDs and tags are not displayed.

**GoreeCloud UI theme parity:** Native Android uses generated Material3 light/dark colors exactly derived from the same attributed GoreeCloud UI CSS token snapshot used by the web app; CI verifies the mapping. Device-level contrast/readability/accessibility acceptance and all other cross-product UI components remain pending.
