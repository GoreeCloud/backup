# GoreeCloud Backup — Feature Inventory

| Capability | Current state | Verification boundary |
| --- | --- | --- |
| Kopia repository status | Implemented in Go API | Requires an installed, connected Kopia CLI; no real repository accepted |
| Encrypted snapshot creation | Implemented via Kopia CLI | Only under configured existing source root; synthetic restore passed, real recovery pending |
| Full 100% archived-file verification | Implemented as authenticated, serialized, user-confirmed API action | Synthetic CLI and HTTP API tests pass; independent restore remains necessary |
| Snapshot listing | Implemented via Kopia JSON CLI | Payload compatibility and pagination need integration tests |
| Pinned Kopia and Rclone upstream source | Included under `third_party/`; file hashes checked | Full transitive license/dependency audit and release packaging pending |
| Rclone remote discovery | Implemented in Go API | Names only; no upload/sync operation |
| Web app | React/TypeScript source | Browser and accessibility acceptance not yet complete |
| Android app | Kotlin/Jetpack Compose read-only source | CI debug APK available for testing; signed production APK and physical-device acceptance pending |
| Rclone backend as Kopia storage | Upstream CLI setup only | Experimental/not maintained upstream; not validated here |
| Contained restore via API and web | Implemented, disabled unless `GCB_RESTORE_ROOT` is configured | An isolated synthetic HTTP restore passed including nested paths/symlinks/Unicode/large files; no arbitrary restore destination or live overwrite |
| Advanced recovery browsing, pruning, retention UI | Planned | Requires safe restore-target handling and nonproduction drills |
| Opt-in Linux systemd scheduled snapshots | Source templates and protected local CLI implemented | Explicit operator installation required; not active or deployed |
| Cross-platform schedule UI, alerts and persistent job management | Planned | Needs durable scheduler, state tracking and secure notifications |
| Android device backup/restore | Planned | Scoped storage/permissions and encrypted transport needed |
| GoreeCloud Mesh, Identity, GoreeShield, Wardveil integration | Requirements identified | No external runtime integration claimed |

Source and tests establish development capabilities, not production backup reliability.
