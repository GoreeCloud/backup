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
| Opt-in Linux systemd scheduled snapshots | Source templates and protected local CLI implemented | Explicit operator installation required; not active or deployed |
| Cross-platform schedule UI, alerts and persistent job management | Planned | Needs durable scheduler, state tracking and secure notifications |
| Android one-file foreground backup | Implemented as opt-in source staging + Kopia snapshot and system-picker client | Synthetic HTTP upload, SHA-256 receipt, encrypt and staging cleanup tested; physical-device/upload TLS acceptance pending |
| Bulk/automatic Android device backup and device restore | Planned | Scoped storage/permissions, policy, durable jobs and recovery drills required |
| GoreeCloud Mesh, Identity, GoreeShield, Wardveil integration | Requirements identified | No external runtime integration claimed |

Source and tests establish development capabilities, not production backup reliability.


**Supply-chain visibility:** Manually invoked Recovery Integration CI also generates a dated-by-workflow resolved-Go-module CycloneDX inventory artifact for the attributed Kopia and Rclone source trees. This records module selection only; full transitive licensing/advisory and release provenance audits remain open.

**Web selected-file backup:** The React workspace now offers the same consent-gated, single-file foreground upload as the Android system-picker client when the private server ingress is configured. Source selection remains explicit; automatic browser directory access, background synchronization and client-side end-to-end encryption are not claimed.
