# GoreeCloud Backup — Roadmap

## Stage A — foundation (current implementation work)

- First-party Go orchestration API with basic authentication and path validation.
- Kopia snapshot/status and Rclone remote-discovery adapters.
- React web and Kotlin/Jetpack Compose Android read-only client.
- Baseline CI, legal attribution, security documentation and branch safeguards.

## Stage B — recoverability before expansion

- Isolated backup and restore test harness, including corruption/partial-transfer test cases.
- Robust job queue, cancellation, detailed progress and repository error mapping.
- Restore browsing and destination-safe recovery workflow with checksums and explicit overwrite protection.
- Configurable policy engine for schedules, retention, exclude patterns and integrity verification.

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
