# GoreeCloud Backup — Roadmap

## Stage A — foundation (current implementation work)

- First-party Go orchestration API with basic authentication, path validation and serialized archive verification.
- Kopia snapshot/status and Rclone remote-discovery adapters.
- Import pinned upstream Kopia and Rclone sources with license notices and a verified local build script; complete transitive audits and release acceptance in later stages.
- React web and Kotlin/Jetpack Compose Android recovery client with opt-in one-file foreground device backup.
- Baseline CI, legal attribution, security documentation and branch safeguards.

## Stage B — recoverability before expansion

- Expand the isolated backup and restore harness: encrypted-pack corruption and disposable missing/truncated-pack regression tests pass locally; lost-credential, real remote-storage interruption, resumable transport and production failure recovery remain open.
- Robust job queue, cancellation, detailed progress and repository error mapping.
- Extend Linux mount-isolated contained restore with pre-extraction content-tree checks, file-level browsing, metadata comparison, offline restore drills, checksums, safe cleanup and robust overwrite safeguards.
- Build on the implemented opt-in Linux systemd timer templates with a configurable policy engine for cross-platform schedules, retention, exclude patterns and integrity verification.

## Stage C — integrations and reliability

- Verified Kopia native storage destinations; independently evaluate Rclone provider compatibility.
- GoreeCloud Identity authorization, GoreeShield retention/consent and Wardveil threat controls.
- Mesh event/API contract after privacy and authorization gates.
- Extend the implemented user-selected foreground Android file backup with secure client pairing, resume/chunking, batch file selection and verified work-managed backup operations.

## Stage D — release readiness

- Operator deployment guide, OS service, secrets provisioning and independent restore evidence.
- Signed Android artifact, device compatibility and accessibility testing.
- Supply chain maintenance, dependency updates, CI gates, branch protection and release signing.

A stage is not complete solely because its source files exist. No release date has been committed.


- Extend opt-in foreground selected-file ingestion (web and Android) with crash-safe private staging, explicit operator inspection/cleanup, resilient resumable transfers and independently verified optional device-side encryption before treating it as a privacy-certified solution.

### Software supply chain

- Expand the implemented resolved-Go-module inventory into a complete, independently checked binary dependency/permissions/licensing/advisory/SBOM audit covering pinned Go engines, Android/Gradle, web/npm, operating-system libraries, CI actions, and distribution artifacts.
