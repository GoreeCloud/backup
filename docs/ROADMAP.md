# GoreeCloud Backup — implementation map

The authoritative product intent for this milestone is `docs/PRODUCT_SCOPE.md`, reproduced directly from the user-provided specification. This map does not assert that a listed product capability has been built merely because an upstream engine offers it.

## Milestone A: local alpha — implemented and published on GitHub

- Go control-plane server, authenticated loopback API, security headers and bounded command execution
- Local responsive dashboard, engine availability, recent job history and operation submission
- Kopia snapshot creation, listing and guarded restoration through the installed engine
- Rclone copy (immutable destination), integrity check, dry-run synchronization and confirmed live synchronization gated by a fresh matching single-use preview
- Command validation, bounded queue admission, restore destination reservation, unit tests, race tests, documented security boundaries and CI workflow\n- Opt-in private, atomic metadata-only job-status journal (no source paths, credentials, engine output or sync approvals), interrupted-job marking after restart, and privacy/recovery tests

## Milestone B: usable local backup product — not yet implemented

- Durable scheduler/database beyond the metadata-only alpha journal; resumable or cancelable job lifecycle, coordinated recovery of interrupted operations, and corruption/migration management
- Setup wizard, repository creation/connection, policy and retention editor, scheduling and notifications
- Structured snapshot browser, file-level restore previews and independent restoration verification
- Provider configuration, credential-handling flows and storage-health diagnostics
- Canonical GC UI component/token adoption, real-browser screenshot audit and accessibility review
- Native Linux service install, desktop client and signed release artifacts

## Milestone C: protected multi-device environment — not yet implemented

- Agent enrollment and authenticated service communications over GoreeCloud VPN
- Granular RBAC and GoreeCloud Identity SSO; trust-boundary-per-repository multi-tenancy
- Immutable storage and separate-copy replication policy orchestration
- Persistent security audit trail, observability, metrics and alerting
- Hardened backup/restore acceptance across Linux and Windows, with recovery drills

## Milestone D: mobile, multi-cloud and ecosystem — not yet implemented

- Native Android app with storage-access-framework and Android background restrictions honored
- Native Apple clients as platform constraints and release resources permit
- Storage browser, mount management, archive workflows, supported provider configuration
- Drive, Search, Gateway, DNS and Identity verified integrations
- Storage cost insights, lifecycle management and optional privacy-preserving assisted operations

## Feature breadth

The 32-section specification includes hundreds of features. Many are **upstream capability targets** (Kopia and Rclone), not direct exposed functions of the current control plane. Features such as scheduling, Android, iOS, multi-tenant SSO, immutable backups, OS images and AI remain work items, not shipped functions.

## Acceptance gates

A successful compile or test is not a stable release. Accept each target only after live integration testing, backup-to-restore proof, threat review, visual screenshot audit, recovery drill, and platform-specific packaging and installation tests. Do not deploy this milestone to a VPS as a production service.

## Verified acceptance evidence (2026-11-03)

- Go build, race detector tests and vet pass in the isolated local development environment.
- Rclone 1.75.2 local-to-local copy, check, sync dry-run, actual sync and post-sync file comparison passed in a disposable directory on an authorized Linux laptop.
- Kopia 0.23.1 signature/checksum verification, encrypted local repository creation, snapshot creation/listing, reserved-path restore, SHA-256 equality and 100% verification **passed directly through the engine CLI**. A subsequent combined API-to-engine smoke test also passed authenticated Kopia backup/list/restore and Rclone copy/check/preview/sync against disposable local data. Real cloud-provider integration remains unverified.
- All original 21 alpha source files were published to public GitHub main at `8921fc2`, fetched and compared exactly against staged source, and the initial GitHub Actions Verify run passed. A separately published Python API-to-engine smoke script was also run successfully. Later commit CI status is evaluated independently.
- See `docs/TEST_EVIDENCE_2026-11-03.md` for the test scope and limitations; `scripts/smoke-local-kopia.sh`, `scripts/smoke-local-rclone.sh` and `scripts/smoke-api-e2e.py` capture reproducible disposable acceptance workflows.
