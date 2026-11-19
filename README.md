# GoreeCloud Backup

**Status: development preview (not released, deployed, or recovery-certified).**

GoreeCloud Backup is a local-first, open-source backup control service with a **React/TypeScript web interface** and a **native Android Kotlin/Jetpack Compose read-only companion**. It coordinates an existing Kopia CLI for client-side-encrypted, versioned snapshots and discovers configured Rclone remotes. The authoritative repository is [GoreeCloud/backup](https://github.com/GoreeCloud/backup).

## What exists

- Go standard-library API: token-gated repository status, JSON snapshot listing, creating encrypted snapshots from subdirectories of an allowlisted root, full-file archive integrity verification, and Rclone remote discovery.
- React web workspace: in-memory token connection, repository status, snapshot list, snapshot creation, cost-aware full-file verification and remote names, with visual tokens sourced from [GoreeCloud UI](https://github.com/GoreeCloud/gc-ui).
- Native Android app: foreground-only authenticated HTTPS status, snapshot count, remote count; no stored credentials or automatic backup.
- Automated Go tests, web build, Android build workflow, security/deployment guidance and roadmap.

**Not implemented:** backup schedules, production recovery certification, repository setup from the UI, Rclone file-sync operations, Android device backup, secure multi-user roles, GoreeCloud Identity/Mesh live integration, signed Android releases, and production operations. No server is deployed by this source.

## Included upstream source and engine builds

Pinned upstream source for **Kopia v0.23.1** and **Rclone v1.75.2** is now maintained under `third_party/kopia/` and `third_party/rclone/`, with their original license files, module pins and copyright notices retained. See [upstream provenance](third_party/UPSTREAMS.md). These are attributed upstream source snapshots with an explicitly documented small security-fixture patch, not independently authored GoreeCloud code. See `third_party/PATCHES.md`.

From the repository root, after installing a supported Go toolchain, run `./scripts/build-upstreams.sh` to build both from pinned source into ignored `build/bin/`. The script verifies imported file hashes before compiling and never invokes upstream installers. Select those binaries with `GCB_KOPIA_BIN=$PWD/build/bin/kopia` and `GCB_RCLONE_BIN=$PWD/build/bin/rclone`; the default still uses separately installed executables on `PATH`. Module dependencies are resolved from audited public Go proxy/checksum infrastructure at build time; full transitive vendor/SBOM review is pending.

## Source structure

| Path | Purpose |
| --- | --- |
| `cmd/backupd/` and `internal/backup/` | First-party Go service/API |
| `web/` | React web application (Vite) |
| `android/` | Kotlin/Jetpack Compose native Android client |
| `docs/` | Architecture, operational safety, features, objectives and roadmap |
| `.github/workflows/` | CI verification |
| `third_party/kopia/`, `third_party/rclone/` | Pinned upstream engine sources, licensing and modules |
| `scripts/build-upstreams.sh` | Checked source builds |

## Prerequisites

Go 1.24+, Node.js 22+, npm, and **separately installed Kopia and Rclone binaries** (either separately installed and verified or built from the included source with `scripts/build-upstreams.sh`). Pinned upstream sources are included in `third_party/`; standalone binaries are still optional runtime prerequisites until built with the included script and selected by explicit environment settings.

Set up the Kopia repository **as the same unprivileged OS user** that will run the service. For a local repository, see `kopia repository create filesystem --path=<outside-source-root>`. For a supported native cloud provider, use Kopia's native repository backend. Kopia's Rclone storage backend may be used manually, e.g. `kopia repository create rclone --rclone-exe=/path/to/rclone --remote-path=name:path`, but upstream labels that backend *experimental/not maintained*; use only after independent compatibility and restoration tests.

Never put a repository password or Rclone configuration into Git. Back up repository connection details and encryption recovery material securely and separately.

## Local development

```bash
# In your own shell, from the repository root:
export GCB_API_TOKEN="$(openssl rand -hex 32)"
export GCB_SOURCE_ROOT="/absolute/path/to/approved-source-parent"
export GCB_LISTEN="127.0.0.1:8787"
# Optional, for contained restore only: export GCB_RESTORE_ROOT="/absolute/private/restore-root"
# Optionally: export GCB_WEB_DIR="$PWD/web/dist"
go run ./cmd/backupd
```

Open a second terminal:
```bash
cd web
npm ci
npm run dev
```

Visit http://127.0.0.1:5173/ and enter the generated access token. The Vite development server proxies only API and health requests to the local Go service. To serve the production web bundle from Go, run `npm run build`, then set `GCB_WEB_DIR` to the absolute `web/dist` path before starting the daemon.

Example authenticated API calls:
```bash
curl -H "Authorization: Bearer $GCB_API_TOKEN" http://127.0.0.1:8787/api/v1/status
curl -H "Authorization: Bearer $GCB_API_TOKEN" http://127.0.0.1:8787/api/v1/snapshots
curl -H "Authorization: Bearer $GCB_API_TOKEN" http://127.0.0.1:8787/api/v1/remotes
curl -X POST -H "Authorization: Bearer $GCB_API_TOKEN" -H "Content-Type: application/json" \
  -d '{"source":"Documents"}' http://127.0.0.1:8787/api/v1/snapshots
# Explicit, potentially expensive full-content verification:
curl -X POST -H "Authorization: Bearer $GCB_API_TOKEN" http://127.0.0.1:8787/api/v1/verify
# Once GCB_RESTORE_ROOT is set, choose an ID from the snapshot listing:
# curl -X POST -H "Authorization: Bearer $GCB_API_TOKEN" -H 'Content-Type: application/json' \
#   -d '{"snapshotId":"<snapshot ID from Kopia>","acknowledge":true}' http://127.0.0.1:8787/api/v1/restore
```

The example assumes an existing `Documents` directory *below* the source root, and an already-connected Kopia repository. Contained restore is opt-in and **never** writes into the source root. Configure an existing directory owned by the least-privileged service account, with no group/other write permission and entirely disjoint from the source root; the service allocates a fresh private workspace for each restore. Review symlinks and recovered file permissions before accessing restored files. Recovery and actual cloud-provider durability remain independently unverified. A 100% verification may incur cloud egress fees; the web UI requires explicit acknowledgement. Verification does not replace a tested restore. Avoid backing up live databases without application-consistent snapshots.

## Android

Import `android/` into Android Studio with JDK 17 and Android SDK 35, then build the `app` module. The client requires an externally secured **HTTPS** endpoint. No production TLS reverse proxy, Android package, device installation or signing has been verified by this repository yet. The client does not upload phone data.

## Tests and security

```bash
go test -race ./...
cd web && npm ci && npm run build
# Android: cd android && ./gradlew :app:assembleDebug (SDK 35)
```

The API binds to **127.0.0.1** by default and refuses startup without a 32+ byte bearer token and an existing source root. For Android, use trusted HTTPS with authentication via an independently configured, access-controlled proxy or private network. **Do not expose plaintext HTTP on public networks.** See [Security](docs/SECURITY.md) and [Architecture](docs/ARCHITECTURE.md). A CI build does not prove restore reliability. Perform a nonproduction restore drill before depending on real data.

## Open source and attribution

The first-party source is **Apache License 2.0**; see [LICENSE](LICENSE). The UI token stylesheet from GoreeCloud UI is MIT-licensed, with attribution in [THIRD_PARTY](docs/THIRD_PARTY.md). Pinned Kopia (Apache-2.0) and Rclone (MIT) source snapshots are bundled in `third_party/`, with original licenses preserved. Their upstream copyrights and identity remain separate from GoreeCloud. Maintain notices and verify licenses/versions when importing upstream source.

The isolated synthetic Kopia encrypt/snapshot/verify/restore script is `scripts/smoke-isolated.sh`; the HTTP integration test is `scripts/smoke-api-isolated.sh`, which also exercises opt-in contained restoration. Both create and remove temporary repositories and do not read existing user repositories. The latter tests token rejection, snapshot creation/listing and 100% content verification through the Go API.

No analytics, advertising trackers or cloud accounts are configured by this project. GoreeShield covers privacy, Wardveil cybersecurity, GoreeCloud Mesh integration, GoreeCloud Identity access control, and GoreeCloud UI design; real cross-platform enforcement beyond the currently implemented local controls remains planned.

## Roadmap

See [Objectives](docs/OBJECTIVES.md), [Features](docs/FEATURES.md) and [Roadmap](docs/ROADMAP.md). Contributions are welcome through reviewable pull requests; please do not file public issues with passwords, cloud paths or personal data.
