# GoreeCloud Backup — Feature Inventory

| Capability | Current state | Verification boundary |
| --- | --- | --- |
| Kopia repository status | Implemented in Go API | Requires an installed, connected Kopia CLI; no real repository accepted |
| Encrypted snapshot creation | Implemented via Kopia CLI | Only under configured existing source root; real restore verification pending |
| Snapshot listing | Implemented via Kopia JSON CLI | Payload compatibility and pagination need integration tests |
| Rclone remote discovery | Implemented in Go API | Names only; no upload/sync operation |
| Web app | React/TypeScript source | Browser and accessibility acceptance not yet complete |
| Android app | Kotlin/Jetpack Compose read-only source | Signed APK and physical device acceptance pending |
| Rclone backend as Kopia storage | Upstream CLI setup only | Experimental/not maintained upstream; not validated here |
| Restoration, pruning, retention UI | Planned | Requires safe restore-target handling and nonproduction drills |
| Continuous schedules and alerts | Planned | Needs scheduler, state tracking and secure notifications |
| Android device backup/restore | Planned | Scoped storage/permissions and encrypted transport needed |
| GoreeCloud Mesh, Identity, GoreeShield, Wardveil integration | Requirements identified | No external runtime integration claimed |

Source and tests establish development capabilities, not production backup reliability.
