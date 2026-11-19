# GoreeCloud Backup — Roadmap

## Stage A — foundation (current implementation work)

- First-party Go orchestration API with basic authentication, path validation and serialized archive verification.
- Kopia snapshot/status and Rclone remote-discovery adapters.
- Import pinned upstream Kopia and Rclone sources with license notices and a verified local build script; complete transitive audits and release acceptance in later stages.
- React web and Kotlin/Jetpack Compose Android recovery client with opt-in one-file foreground device backup.
- Baseline CI, legal attribution, security documentation and branch safeguards.

## Stage B — recoverability before expansion

- Expand the isolated backup and restore harness: encrypted-pack corruption and disposable missing/truncated-pack regression tests pass locally; wrong-password rejection now passes in an isolated copied-repository drill; actual lost-key recovery, real remote-storage interruption, resumable transport and production failure recovery remain open.
- Robust job queue, cancellation, detailed progress and repository error mapping.
- Extend Linux mount-isolated contained restore with pre-extraction content-tree checks, file-level browsing, metadata comparison, offline restore drills, checksums, safe cleanup and robust overwrite safeguards.
- Build on the implemented opt-in Linux snapshot and full-content integrity-check timer templates with a configurable policy engine for cross-platform schedules, retention, exclude patterns and offline recovery testing.

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


- The selected-file plaintext staging root is now deliberately separate from the source/restore roots, with fail-closed orphan detection. Future work: crash-safe per-upload journaling, optional encrypted scratch storage, auditing, quota controls and lifecycle maintenance with explicit retention policy.


- Replace coarse preview admin/view bearer secrets with verified GoreeCloud Identity accounts, short-lived credentials, per-user resource permissions, rotation, device pairing and audited authorization policies before multiuser production use.


- Extend the passing isolated Rclone-to-local encrypted repository mirror/restore check to disposable SFTP/S3/NAS provider compatibility tests. Design consistent mirror checkpoints and resumable, least-privilege, verified transfers before implementing remote backup orchestration; never run destructive sync/prune on the only recoverable repository.


- Continue from verified Android light/dark GoreeCloud UI color parity toward responsive layouts, accessible touch targets, tested typography, themes/accent options, screen-reader acceptance, Motion/accessibility preferences and centralized reusable native components.


- Extend the limited no-new-AWS/GitHub-key credential guard to verified multi-pattern/entropy scanning with false-positive triage and upstream migration support. Seek authoritative owner evidence on historical GitHub secret-scanning alert #1; neither inherited examples nor old commits are cleared by a new-source scanner.

- Eliminate the remaining external Kopia path lookup race using a verified descriptor-preserving source handoff, or enforce a stronger per-job OS sandbox, dedicated UID and immutable parent paths. Validate hard process crashes, partial disk failures and orphaned plaintext management before multiuser deployment.

- Beyond the passing disposable local and loopback SFTP Kopia/Rclone recovery drills, test controlled S3-compatible object stores, provider quotas, interrupted and retried transfers, real offsite credentials, per-user permissions, untrusted remote host keys, immutable repository mirrors and consistency-safe checkpoints. Never delete the only recoverable repository.

- Extend local, SFTP-loopback and experimental S3-loopback recovery tests to independently provisioned, disposable S3-compatible providers; exercise timeouts, multipart failure, quotas, versioning, immutable retention and consistent copy checkpoints before enabling remote automation.

- Advance the new manual unsigned compiled-engine evidence into independently repeated byte-for-byte reproducible builds across isolated Linux runners, verified license and advisory reviews, signed release artifacts and protected attestations before shipping production binaries.

- Extend read-only local preflight into opt-in documented operational acceptance gates that incorporate verified offsite location, retention policy, key-recovery rehearsal, last successful snapshot age, meaningful logging and independent byte-for-byte restore evidence. Do not conflate metadata availability with recoverability.

- Extend the optional dual-runner Linux AMD64 hash comparison into signed, verifiable build attestations, repeat builds across independent runner providers, reproducibility tests for Android/web and additional OS/architecture targets, with complete dependency licensing and vulnerability triage before stable distribution.

- Extend secure local credential handling beyond pinned per-invocation token reads: harden configuration parent ownership and unprivileged service UIDs, implement auditable token rotation/revocation and purpose-scoped native pairing through GoreeCloud Identity and GoreeShield with Wardveil protections.

- Advance the backup-specific recovery timeline with accessible real-browser/mobile viewport acceptance, snapshot age/retention summaries grounded in server data, safe search/filter by anonymized label, and independent recovery success evidence. Never depict a listed backup as verified or recoverable without an actual restore drill.

- Expand browser-focused robustness coverage into real-browser integration, aborting expensive requests on navigation or disconnect, meaningful progress/cancellation for lengthy operations, server-side rate limiting and privacy-preserving structured error codes. Keep all current bearer tokens transient and never display untrusted raw error bodies.
