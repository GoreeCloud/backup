# GoreeCloud Backup — Roadmap

## Stage A — foundation (current implementation work)

- First-party Go orchestration API with basic authentication, path validation and serialized archive verification.
- Kopia snapshot/status and Rclone remote-discovery adapters.
- Import pinned upstream Kopia and Rclone sources with license notices and a verified local build script; complete transitive audits and release acceptance in later stages.
- React web and Kotlin/Jetpack Compose Android read-only client.
- Baseline CI, legal attribution, security documentation and branch safeguards.

## Stage B — recoverability before expansion

- Expand the isolated backup and restore harness: encrypted pack corruption detection now passes; partial-transfer, lost-credential and remote-storage failure cases remain open.
- Robust job queue, cancellation, detailed progress and repository error mapping.
- Extend the implemented contained restore with file-level browsing, metadata comparison, offline restore drills, checksums and robust overwrite safeguards.
- Build on the implemented opt-in Linux systemd timer templates with a configurable policy engine for cross-platform schedules, retention, exclude patterns and integrity verification.

## Stage C — integrations and reliability

- Verified Kopia native storage destinations; independently evaluate Rclone provider compatibility.
- GoreeCloud Identity authorization, GoreeShield retention/consent and Wardveil threat controls.
- Mesh event/API contract after privacy and authorization gates.
- Secure client pairing and Android foreground/work-managed backup operations.

## Stage D — release readiness

- Operator deployment guide, OS service, secrets provisioning and independent restore evidence.
- Signed Android artifact, device compatibility and accessibility testing.
- Supply chain maintenance, dependency updates, CI gates, branch protection and release signing.

A stage is not complete solely because its source files exist. No release date has been committed.
