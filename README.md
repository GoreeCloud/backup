# GoreeCloud Backup

**Status: development preview (not released, deployed, or recovery-certified).**

GoreeCloud Backup is a local-first, open-source backup control service with a **React/TypeScript web interface** and a **native Android Kotlin/Jetpack Compose recovery and selected-file backup client**. It coordinates an existing Kopia CLI for client-side-encrypted, versioned snapshots and discovers configured Rclone remotes. The authoritative repository is [GoreeCloud/backup](https://github.com/GoreeCloud/backup).

## What exists

- Go standard-library API: token-gated repository status, JSON snapshot listing, creating encrypted snapshots from subdirectories of an allowlisted root, full-file archive integrity verification, and Rclone remote discovery and opt-in capacity reporting on supported remotes.
- React web workspace: in-memory token connection, repository status, snapshot list, snapshot creation, cost-aware full-file verification and remote names, with visual tokens sourced from [GoreeCloud UI](https://github.com/GoreeCloud/gc-ui).
- Native Android app: foreground-only authenticated HTTPS status, snapshot browsing and explicitly selected one-file Kopia backup with an opt-in private server inbox; no stored credentials or background backup.
- Automated Go tests, web build, Android build workflow, security/deployment guidance and roadmap.

**Not implemented:** backup schedules, production recovery certification, repository setup from the UI, Rclone file-sync operations, automatic/bulk Android device backup, secure multi-user roles, GoreeCloud Identity/Mesh live integration, signed Android releases, and production operations. No server is deployed by this source.

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
| `cmd/backupctl/` and `packaging/systemd/` | Protected local snapshot client and opt-in Linux schedule templates |
| `cmd/backupctl/` and `packaging/systemd/` | Protected local snapshot client and opt-in Linux schedule templates |

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
# Optional read-only provider quota query; may contact a configured cloud remote:
# curl --get -H "Authorization: Bearer $GCB_API_TOKEN" \
#   --data-urlencode 'remote=remote-name:' http://127.0.0.1:8787/api/v1/remotes/about
curl -X POST -H "Authorization: Bearer $GCB_API_TOKEN" -H "Content-Type: application/json" \
  -d '{"source":"Documents"}' http://127.0.0.1:8787/api/v1/snapshots
# Explicit, potentially expensive full-content verification:
curl -X POST -H "Authorization: Bearer $GCB_API_TOKEN" http://127.0.0.1:8787/api/v1/verify
# Once GCB_RESTORE_ROOT is set, choose an ID from the snapshot listing:
# curl -X POST -H "Authorization: Bearer $GCB_API_TOKEN" -H 'Content-Type: application/json' \
#   -d '{"snapshotId":"<snapshot ID from Kopia>","acknowledge":true}' http://127.0.0.1:8787/api/v1/restore
```

The example assumes an existing `Documents` directory *below* the source root, and an already-connected Kopia repository. Contained restore is opt-in and **never** writes into the source root. Configure an existing directory owned by the least-privileged service account, with no group/other write permission and entirely disjoint from the source root; the service allocates a fresh private workspace for each restore. Review symlinks and recovered file permissions before accessing restored files. Recovery and actual cloud-provider durability remain independently unverified. A 100% verification may incur cloud egress fees; the web UI requires explicit acknowledgement. Verification does not replace a tested restore. Avoid backing up live databases without application-consistent snapshots.

## Optional scheduled snapshots on Linux

A first-party `backupctl` binary allows **explicitly configured** systemd timers to call the authenticated, loopback-only backup API. It never accepts an API token on the command line, refuses non-loopback endpoints and HTTP redirects, and reads a private file supplied by the operator. No timers, credentials, accounts or services are installed/enabled by the repository.

Build using `go build -o build/bin/goreecloud-backupctl ./cmd/backupctl`. See [the opt-in systemd deployment templates](packaging/systemd/README.md) before installing or enabling anything. Timed backups are only as consistent as the applications' source snapshots; a timer firing is not proof of successful recovery.

## Optional scheduled snapshots on Linux

A first-party `backupctl` binary allows **explicitly configured** systemd timers to call the authenticated, loopback-only backup API. It never accepts an API token on the command line, refuses non-loopback endpoints and HTTP redirects, and reads a private file supplied by the operator. No timers, credentials, accounts or services are installed/enabled by the repository.

Build using `go build -o build/bin/goreecloud-backupctl ./cmd/backupctl`. See [the opt-in systemd deployment templates](packaging/systemd/README.md) before installing or enabling anything. Timed backups are only as consistent as the applications' source snapshots; a timer firing is not proof of successful recovery.

## Android

Import `android/` into Android Studio with JDK 17 and Android SDK 35, then build the `app` module. The client requires an externally secured **HTTPS** endpoint. No production TLS reverse proxy, Android package, device installation or signing has been verified by this repository yet. The native Android client now lists recent recovery points, verifies archived files with explicit cost acknowledgement, and requests opt-in contained server-side restoration. It keeps credentials in volatile memory and requires trusted HTTPS without redirects; one explicitly selected Android system-picker file can be uploaded only when the operator enables a private ingest directory (32 MiB maximum); automatic device-file scanning, background sync and remote provisioning remain unimplemented. The client never uploads phone data without a user-chosen file and explicit consent. A debug APK and web preview bundle are available as short-lived artifacts from successful [Verify workflow](https://github.com/GoreeCloud/backup/actions/workflows/verify.yml) runs; these are testing builds, **not** signed production releases. Inspect the associated commit and workflow before installing a debug artifact.

## Tests and security

```bash
go test -race ./...
cd web && npm ci && npm run build
# Android: cd android && ./gradlew :app:assembleDebug (SDK 35)
```

The API **requires** a numeric loopback bind address (`127.0.0.1` or `[::1]`) and refuses wildcard/public binding even when `GCB_LISTEN` is overridden and refuses startup without a 32+ byte bearer token and an existing source root. For Android, use trusted HTTPS with authentication via an independently configured, access-controlled proxy or private network. **Do not expose plaintext HTTP on public networks.** See [Security](docs/SECURITY.md) and [Architecture](docs/ARCHITECTURE.md). A CI build does not prove restore reliability. Perform a nonproduction restore drill before depending on real data.

## Open source and attribution

The first-party source is **Apache License 2.0**; see [LICENSE](LICENSE). The UI token stylesheet from GoreeCloud UI is MIT-licensed, with attribution in [THIRD_PARTY](docs/THIRD_PARTY.md). Pinned Kopia (Apache-2.0) and Rclone (MIT) source snapshots are bundled in `third_party/`, with original licenses preserved. Their upstream copyrights and identity remain separate from GoreeCloud. Maintain notices and verify licenses/versions when importing upstream source.

The dedicated `scripts/smoke-corruption-isolated.sh` test creates a disposable encrypted repository, confirms its healthy contents, flips a byte in a temporary encrypted pack, and verifies that a fresh integrity check rejects it. This never touches existing backup repositories and is also part of the manual CI recovery workflow.

The isolated synthetic Kopia encrypt/snapshot/verify/restore script is `scripts/smoke-isolated.sh`; the HTTP integration test is `scripts/smoke-api-isolated.sh`, which also exercises opt-in contained restoration. These now cover repeated/deduplicated snapshots, nested directories, symlinks, Unicode filenames and multi-megabyte synthetic fixtures. Both create and remove temporary repositories and do not read existing user repositories. For a slower, repeatable validation from pinned source, use the **Recovery Integration (manual)** GitHub Actions workflow; it is not scheduled or deployed automatically and may consume CI minutes. The latter tests token rejection, snapshot creation/listing and 100% content verification through the Go API.

No analytics, advertising trackers or cloud accounts are configured by this project. GoreeShield covers privacy, Wardveil cybersecurity, GoreeCloud Mesh integration, GoreeCloud Identity access control, and GoreeCloud UI design; real cross-platform enforcement beyond the currently implemented local controls remains planned.

## Roadmap

See [Objectives](docs/OBJECTIVES.md), [Features](docs/FEATURES.md) and [Roadmap](docs/ROADMAP.md). Contributions are welcome through reviewable pull requests; please do not file public issues with passwords, cloud paths or personal data.


### Linux contained-restore sandbox

The development API requires [Bubblewrap](https://github.com/containers/bubblewrap) (`bwrap`) for the opt-in contained restore on Linux. The Kopia restore subprocess sees the host filesystem read-only, with only a new private restore workspace writable. Configure a private restore root separate from the source; do not run the API as root. Web and Android report restore disabled when the sandbox executable is unavailable. Remote repositories still require network access, and this isolation is not a substitute for recovery drills, independent permissions testing or untrusted multi-tenant workload separation.

### Opt-in foreground Android file backup

This **development preview** can back up one file deliberately chosen through Android’s system document picker. The Android client uses an authenticated, trusted-HTTPS `POST /api/v1/mobile/files` with an in-memory token and a bounded 32 MiB stream. No broad storage permissions, automatic file scanning, background sync or stored phone-file copies are used.

The server operator must first create a **private directory with permissions 0700 inside the configured source root**, such as `/srv/backup-sources/mobile-ingest`, and set `GCB_MOBILE_INGEST_ROOT` to its canonical absolute path. The API stays disabled by default. Each upload is staged in a randomly named private subdirectory, encrypted into the already-connected Kopia repository, and removed from the server’s temporary plaintext staging area. The uploaded source file is not retained as an ordinary server file. The client cannot choose a server path, upload an empty file or bypass explicit consent. The API supports one file at a time; it reports successful command completion, **not** independent recovery certification. The original selected document remains on the phone. A configured HTTPS reverse proxy/tunnel is mandatory for remote Android access; the Go service never exposes plaintext bearer HTTP to the LAN.

Upload credentials and phone contents must not be logged, copied into Git, or treated as test fixtures. A failed or interrupted upload may not produce a valid snapshot; verify and restore synthetic samples before relying on real data. No deployed backend or physical-device acceptance has been established by this change.


### Source module inventory

After the manual `Recovery Integration` workflow builds the pinned Kopia and Rclone sources, it publishes a seven-day `goreecloud-backup-resolved-go-modules` CycloneDX JSON artifact. For a local developer with Go 1.26 and complete reviewed Go module metadata, run `python3 scripts/go_module_sbom.py` (writes only beneath ignored `build/sbom/`). Pure parser/ordering tests: `python3 -m unittest discover -s tests -p 'test_go_module_sbom.py'`. This inventory is deliberately **not** labeled a complete binary/dependency-license/security audit.

### Web selected-file backup

The React web workspace can also back up **one explicitly selected file** (up to 32 MiB) through the existing authenticated selected-file ingress endpoint, when the server operator has opted in with `GCB_MOBILE_INGEST_ROOT`. It never reads directories or uploads other files automatically. Remote access must be HTTPS; only local loopback HTTP is accepted for development. Upload requests explicitly refuse HTTP redirects to protect bearer credentials and require an in-page consent checkbox. The browser does not persist selected files or tokens across reloads.

**Privacy boundary:** TLS protects transit to the operator-controlled server, but the server temporarily holds the selected file in plaintext before Kopia encrypts it into the repository. This is *not* device-side end-to-end encryption. The application removes staged plaintext on normal completion and most failures, but an abrupt process/host crash could leave private staging files for operator review and cleanup. Use a hardened, least-privileged server and do not rely on this feature for irreplaceable data until independent recovery acceptance has passed.


The browser computes SHA-256 of the user-selected file in the secure browser context and checks it against the server's received-bytes receipt before reporting success. This detects accidental upload corruption or mismatched responses but is **not proof of future repository recoverability** or end-to-end encryption.

### Missing/truncated encrypted-pack resilience drill

The manual Recovery Integration CI includes `scripts/smoke-incomplete-pack-isolated.sh`, which creates a disposable encrypted Kopia repository, verifies byte-copied local mirror repositories before damage, then simulates both a missing pack and an interrupted/truncated pack. Fresh-cache full-content verification must fail closed in both cases. No external repositories, cloud provider, credentials or user data are accessed. This tests detection of incomplete copies, **not** automatic repair, resumable transport, or production disaster recovery.

### Opt-in local integrity CLI

The first-party Linux `backupctl` now supports `verify --accept-provider-costs` as well as `snapshot --source=Documents`. The new command authenticates over numeric-loopback HTTP using the existing private token file, invokes the server's full Kopia file-read verification, rejects redirects and malformed/unconfirmed responses, and never exposes the bearer token on the command line. A **separate, disabled-by-default** monthly systemd timer template is available in `packaging/systemd/`. Verification can read every cloud-backed file and may incur charges, so the cost acknowledgement is mandatory and the timer must be explicitly installed/enabled. A successful verification is not a substitute for independent restoration testing.

### Lost repository passphrase regression (synthetic only)

`scripts/smoke-lost-credential-isolated.sh` creates a disposable encrypted Kopia repository, verifies a copied mirror with the correct synthetic password, rejects a new connection with an intentionally wrong passphrase without a prompt or indefinite timeout, and verifies the same mirror again using a fresh correct connection. Manual Recovery Integration CI runs this alongside missing/truncated pack checks. This demonstrates password protection and preservation of repository contents after an incorrect attempt; **it cannot recover an actually lost encryption key or substitute for secure credential escrow and independently tested restores**.
