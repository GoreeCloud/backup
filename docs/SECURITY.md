# Security policy and limitations

**Development only. Do not use as an externally exposed production recovery service.**

- Startup requires `GCB_API_TOKEN` with 32 or more bytes. Generate one from a cryptographically secure random source and provide it through a protected secret mechanism, never Git or HTTP URLs.
- Default binding is `127.0.0.1:8787`. Android requires trusted HTTPS. Use a narrowly scoped TLS proxy, auth/rate-limit controls and/or a private VPN; no reverse proxy is bundled.
- Token is held in in-memory React and Android state. Neither client persists it. Do not enter tokens into untrusted pages or transmit them across plaintext networks.
- Go never invokes a shell; external commands are fixed-argument Kopia/Rclone commands with execution timeouts and bounded standard output. The API bearer secret is removed from all child-process environments. Do not put hostile wrappers in the executable search path.
- The API is single-admin and lacks per-user accounts, authorization roles, rate limits, full audit logs and key rotation workflow. Integration with GoreeCloud Identity and Wardveil remains planned.
- The source-root containment check resolves symlinks but does not prevent concurrent filesystem mutation (TOCTOU). Keep the tree immutable to untrusted users for the duration of a backup.
- Contained restore is disabled by default and requires an existing private `GCB_RESTORE_ROOT` disjoint from the source tree; every restore receives a new owner-only workspace, with no caller-selected destination or overwrites. Symlinks inside recovered files may still point outside the workspace and must be treated as untrusted before use. Filesystem mutation races remain a risk if untrusted users can alter restore-root ancestors; avoid shared mutable parent directories.
- Archive verification reads all snapshot files only on explicit request, is serialized with snapshot creation and may incur transfer costs. API success requires an upstream success exit, zero reported errors and evidence of at least one file verified. Kopia counts deduplicated unique objects separately from total file references across repeated snapshots; these totals are not expected to match.
- Real restore integrity, retention deletion safety, disaster recovery and cloud backend compatibility are **unverified**. Test isolated backups and restores before trusting any repository.
- The bundled upstream engine source is version-pinned; `scripts/build-upstreams.sh` checks its SHA256 file manifest, uses a checksum-verified public Go module proxy and does not run upstream installers. A complete transitive malware/license/SBOM audit is pending; do not treat hash checking alone as certification.
- No telemetry or trackers are intentionally included. GoreeShield-specific consent/retention policies and Mesh data exchange require separate control verification.

For private vulnerability reports, use GitHub's private vulnerability reporting if it is enabled on this repository; otherwise contact the maintainer through a secure established channel. Do not publish exploit details, access tokens or private storage contents in issues.

## Threat-model priorities

1. Stolen bearer token / unrestricted admin API; mitigate through TLS, least access, secret rotation and future Identity integration.
2. Repository secret compromise; isolate service user, file permissions, separate secure recovery material.
3. Data corruption, incomplete backup or undecryptable archive; require proof-of-restore, checksums and audited retention operations.
4. Untrusted Rclone remote or upstream dependency; verify versions, provenance, licenses and advisories; pin audited release artifacts.
5. Arbitrary filesystem access via symlink races; constrain source ownership and implement stronger native filesystem containment before multi-user exposure.

## Imported upstream test fixture alert

A public imported Kopia S3 test fixture triggered secret-scanning alert #1 for an AWS-style temporary access key ID. It matches an upstream public test line inside an expired-session credentials provider. Current validity is unknown. The working-tree source replaces its credential-shaped literals with invalid placeholders and records the difference in `third_party/PATCHES.md`. The historical commit remains; potential credential owners must verify expiry and rotate/revoke if appropriate. Do not dismiss the alert or claim revocation merely because the source was patched.

## Scheduled local snapshot client

The optional Linux `backupctl` reads the bearer credential from a private, regular file (0600 or stricter), never from command-line flags, and refuses remote/non-numeric/non-loopback endpoints, proxy use and HTTP redirects. The systemd timer/service templates remain **disabled until deliberately installed** and require systemd credentials, an existing service account and reviewed paths. Neither repository secrets nor user-specific schedules are stored in Git. The local service must enforce its own authentication, and a successful timer run alone does not certify recoverability.

## Scheduled local snapshot client

The optional Linux `backupctl` reads the bearer credential from a private, regular file (0600 or stricter), never from command-line flags, and refuses remote/non-numeric/non-loopback endpoints, proxy use and HTTP redirects. The systemd timer/service templates remain **disabled until deliberately installed** and require systemd credentials, an existing service account and reviewed paths. Neither repository secrets nor user-specific schedules are stored in Git. The local service must enforce its own authentication, and a successful timer run alone does not certify recoverability.

## Repeatable recovery acceptance (synthetic only)

The optional workflow `recovery-manual.yml` builds the pinned Kopia/Rclone sources on an ephemeral runner and exercises two synthetic recovery scripts, including duplicated backups, Unicode and symlinks. It is manual-only to avoid unexpected cloud, build or CI costs. Neither script reads real user repositories or credentials. Success does not guarantee recovery of live databases, cloud object stores, changed permissions, enormous files, interrupted writes or an actual disaster.

## API and contained-restore hardening

- Authenticated requests reject duplicate or comma-joined Authorization header values to avoid ambiguity across HTTP proxies. Snapshot and contained-restore JSON requests require a single explicit `Content-Type: application/json` header; malformed or duplicated content-type fields fail closed.
- The optional built web app is resolved to a canonical directory at startup. Static asset serving rejects files whose symlink targets resolve outside that directory and never exposes directory listings.
- After extraction, the contained-restore workspace is audited before being announced to clients. The audit rejects absolute, dangling, out-of-workspace symlink targets and special files; a failed audit removes the temporary workspace and fails the request. Internal relative links remain supported. This may reject backups that intentionally contain dangling links.
- **Recovery defense-in-depth:** the symlink audit runs after extraction and cannot independently prevent unsafe writes. The Linux restore subprocess now runs in a Bubblewrap read-only-host mount namespace with only the private recovery workspace writable. This mount isolation is a stronger boundary against host-file writes, but it does not validate malicious repository contents before extraction, restrict all network activity or establish safe multi-tenant recovery. Keep a least-privilege service account and the independent restore acceptance gate.

## Android companion client

The native recovery companion rejects URL paths, userinfo, query strings, fragments and non-HTTPS endpoints; it does not follow redirects with bearer credentials, limits JSON responses to 1 MiB and keeps credentials in volatile process memory. Background or automatic Android backup, Keystore-based account pairing, full-device restore and audited background jobs are not implemented. A user may explicitly choose one Android file through the system picker for consent-gated foreground upload when a private server ingest directory is configured. The app excludes its window from screenshots and recent-apps thumbnails by default. The optional server-side verification and contained-restore actions require explicit in-app consent and remain governed by backend validation and capacity limits.

## Rclone provider-capacity boundary

The optional quota endpoint uses fixed Rclone CLI arguments and accepts only a remote name matching an existing configured remote. It rejects flags, destination paths and duplicate remote query parameters; it never accepts raw Rclone commands or transfers backup content. It caps execution time and JSON output and allows only one remote inspection per process. A capacity check may contact an external cloud provider, expose account quota to an authenticated user, and incur provider costs; it is never performed automatically by the web UI. Unsupported or failing backends must not be treated as zero available capacity.

## Encrypted-pack corruption regression

`smoke-corruption-isolated.sh` creates a newly provisioned local Kopia repository under `mktemp`, snapshots deterministic synthetic bytes, validates the healthy repository, then flips one byte within its largest non-configuration encrypted pack. It switches to a fresh cache before verifying that tampering is detected. The test is included only in the **manually dispatched** recovery workflow to avoid unexpected CI costs. No real repositories or upstream credential material are accessed or modified. Passing this test does not prove recoverability after malicious modification of every object, deletion, interrupted writes or remote-provider corruption.

## Explicit loopback-only listener

The unencrypted Go API never binds directly to a wildcard, public, or LAN address. The `GCB_LISTEN` startup setting accepts only numeric `127.0.0.1:PORT` or `[::1]:PORT`, with a valid port from 1 to 65535; `ConfigFromEnv` and `NewHandler` reject invalid nonempty addresses. Use an independently hardened TLS proxy/private tunnel for remote clients. Plaintext HTTP exposure to a LAN or public internet is unsupported. This safeguard is not a TLS proxy, authentication rate limiter or multi-user Identity integration.

## Linux mount-isolated recovery (development preview)

When contained restoration is configured, the Linux API invokes Kopia through Bubblewrap rather than directly. The restore subprocess receives a read-only view of the host filesystem, a single writable bind mount of its freshly allocated private workspace, and dedicated private cache/temp directories. It cannot write into other host filesystem paths through a recovered symlink. The server reports restore capability only when Bubblewrap is available. This **does not** isolate cloud network access, prevent reads from files accessible to the service UID, guarantee physical-device readiness, replace a least-privileged service account, or constitute a hardened multi-tenant sandbox. The service fails the restore if Bubblewrap is unavailable or refuses to start.
## Recovery CI sandbox capability check

The first GitHub-hosted recovery job for the new Bubblewrap mount isolation returned HTTP 502 during the API restore step, despite passing a flawed probe that interpreted any failed Bubblewrap command as a successful outside-write denial. The probe is now fail-closed: it requires a positive in-workspace execution marker and verifies the outside sentinel remains unchanged. If a CI runner restricts user namespaces, the job must fail with its actual diagnostic instead of claiming confinement. A passing local test does not establish portability to hosted runners.


### GitHub-hosted recovery prerequisite (Ubuntu 24.04)

The hosted failure on recovery run `44529992830` was traced to `bwrap: setting up uid map: Permission denied` on the disposable Ubuntu 24.04 runner. The manual workflow now loads Ubuntu's distribution-provided `bwrap-userns-restrict` AppArmor profile for Bubblewrap **only on the ephemeral runner**, and positively tests both sandbox execution and an outside-workspace write denial before running expensive recovery tests. It does not set `kernel.apparmor_restrict_unprivileged_userns=0` or modify the user's devices. The API's `restoreEnabled` capability now requires an executable namespace probe rather than merely finding the binary. If a host denies user namespaces even with the profile, restores fail closed and operator configuration remains required.

## Mobile selected-file upload boundary

This opt-in endpoint is disabled unless `GCB_MOBILE_INGEST_ROOT` is an existing, empty, canonical private 0700 directory outside and non-overlapping with both the source and restore roots. Only authenticated clients presenting explicit consent and a validated base64url-encoded UTF-8 leaf filename may stream a single file, up to 32 MiB, via `application/octet-stream`. The API stages bytes in owner-only private files, serializes the Kopia snapshot job, and removes plaintext staging after completion or error. It neither accepts caller-selected server paths nor stores Android URIs/long-term tokens. The Android client uses the system picker without broad storage permissions and sends file bytes over its validated trusted HTTPS connection. Review parent-folder permissions and available disk space; interrupted uploads, quotas, application-data consistency, and real recovery acceptance remain operational risks. No mobile-upload request is made unless a user explicitly selects a document and confirms the transfer.

## Nonpersistent GitHub checkout credentials

GitHub Verify and manually triggered recovery CI use `actions/checkout@v4` with `persist-credentials: false` and their existing `contents: read` token permission. This avoids leaving the job-scoped GitHub credential in the checkout Git configuration while source builds, third-party dependencies, or test scripts execute. The job does not need authenticated Git pushes or checkout-triggered writes. This control reduces credential exposure but cannot guarantee all dependencies or workflow actions are harmless; source provenance and explicit review still apply.

## Workflow action provenance and immutable pinning

The `Verify` and manually triggered `Recovery Integration` workflows pin all externally fetched GitHub Actions to complete 40-character Git commit IDs, rather than mutable major-version labels. The commits were resolved from the official GitHub Git ref/tag APIs on November 19, 2026 for `actions/checkout` v4, `actions/setup-go` v5, `actions/setup-node` v4, `actions/setup-java` v4, `actions/upload-artifact` v4, `android-actions/setup-android` v3 and `gradle/actions/setup-gradle` v4 (annotated v4.4.3 target). Version labels are retained as comments beside the pinned commits. To update any action, review the upstream source/change history, security advisories, permissions and compatibility, then update the exact commit and rerun all required CI and manual recovery validation. Pinning reduces moving-tag supply-chain risk but does **not** guarantee these dependencies are free of vulnerabilities or malicious code.

## Foreground selected-file upload integrity receipt

The Android client computes a SHA-256 digest while streaming a chosen file over trusted HTTPS. After Kopia snapshot creation and temporary plaintext cleanup, the API returns the number of bytes received and a SHA-256 digest of the uploaded plaintext. Android refuses to report success if either differs from the locally streamed bytes. This detects some silent transport or staging discrepancies and does not require sending a separate plaintext checksum in a header. The digest is transmitted only within the authenticated HTTPS response and is not persisted by the app. A matching receipt alone does **not** prove the encrypted repository remains durable or restorable; synthetic restore testing and real recovery drills remain necessary. Plaintext SHA-256 fingerprints can disclose file identity if leaked, so do not log or publish them.

## Browser upload and bearer-token controls

The browser refuses to connect the bearer token on a non-loopback plaintext origin, prevents cross-origin redirects for authenticated API requests, and uploads only an explicitly selected and consented document to the same-origin opt-in ingress endpoint. Filename bytes are encoded as base64url UTF-8 rather than inserted directly into HTTP headers. A local 32 MiB ceiling matches server enforcement, and the selected `File` object and bearer token stay in memory rather than localStorage. The operator's server **temporarily receives plaintext** before Kopia encryption. Unexpected crashes can leave private staged files; use a dedicated private staging root, least privilege, appropriate secure cleanup procedures and independent recovery tests. Never confuse HTTPS transport confidentiality with end-to-end encryption.


The browser computes SHA-256 of the user-selected file in the secure browser context and checks it against the server's received-bytes receipt before reporting success. This detects accidental upload corruption or mismatched responses but is **not proof of future repository recoverability** or end-to-end encryption.

## Continuous enforcement of immutable workflow actions

The first resolved Go module inventory workflow added an `actions/upload-artifact@v4` reference after initial immutable-action pinning. The artifact uploader is now pinned to its verified official v4 commit, and a first-party static check in the required `docs` CI job rejects mutable or abbreviated third-party action references in all GitHub workflow YAML files. This guard runs without installing actions or dependencies, and its unit tests reject floating tags/branches and truncated SHAs. A passing checker is not a substitute for examining new upstream action source and permissions before adoption.

## Incomplete encrypted-pack recovery test

The manual synthetic recovery workflow now includes `scripts/smoke-incomplete-pack-isolated.sh`. It first creates and verifies an encrypted local Kopia repository and verifies two clean copied mirrors, then deletes one selected encrypted pack in one mirror and truncates the corresponding pack in the other. With independent cache directories, Kopia verification must not report either damaged copy as healthy. The drill only operates under a freshly allocated temporary directory and removes it afterward. A successful regression test demonstrates **detection**, not auto-repair, cloud provider durability, resume guarantees or real-dataset recovery.

## Optional scheduled integrity verification

The local `backupctl verify` command requires an explicit `--accept-provider-costs` flag before reading credentials or contacting the API. It uses the same private on-disk bearer token and no-proxy/redirect numeric-loopback HTTP client as scheduled snapshots, and rejects unsuccessful or malformed verification results without leaking secrets. A separate monthly systemd timer unit remains **disabled by default** and is not installed by source commits. Enabling it explicitly authorizes potentially expensive recurring cloud reads; inspect provider tariffs and budget policy before use. Neither an HTTP 200 alone nor a timer execution is proof of a recoverable archive. Independently restore synthetic and representative user files before operational reliance.

## Wrong repository credential acceptance test

The synthetic `scripts/smoke-lost-credential-isolated.sh` creates and verifies a disposable Kopia repository, verifies its copied mirror with the correct synthetic password, and attempts a fresh connection with a different password, no keyring and no interactive input. A successful incorrect-password connection or a timed-out attempt is a test failure. After rejection, a separate correct connection must still verify the encrypted data. This tests fail-closed authentication and non-destruction in a disposable local filesystem repository. It does not establish recovery from truly lost credentials, credential rotation, hardware-backed storage, or cloud-provider availability. Never put personal encryption keys in CI fixtures.

## Temporary plaintext staging isolation (migration required)

From this change forward, `GCB_MOBILE_INGEST_ROOT` is **not allowed under `GCB_SOURCE_ROOT`**, nor is either permitted to contain the other or overlap a configured restore root. The prior preview configuration placed upload staging under source and could allow a manual or broad scheduled source snapshot to ingest plaintext that had not yet been deleted. This update breaks that unsafe configuration intentionally. Create a fresh empty 0700 private directory outside both trees. Any leftover files from interrupted or crashed ingestion are preserved for explicit operator review and cause startup to fail; the service does not silently purge them. Never store repository encryption keys in the same location. Plaintext remains present during the foreground stream and snapshot, so full disk encryption and least-privileged host controls still matter. This change neither migrates real files nor runs cleanup on live storage.


## Preview least-privilege viewer token

The optional `GCB_VIEW_TOKEN` is an independently generated, at-least-32-byte token that must differ from the administrator `GCB_API_TOKEN`. It authorizes only the metadata-oriented status/snapshot/remote-list and redacted-info GET routes. Snapshot writes, restoration, uploads, full-content verification and provider quota inspection require the admin credential; duplicate or ambiguous Authorization headers fail closed. A viewer response never exposes the configured server source root. Both secrets are stripped from launched Kopia/Rclone child environments. The browser and native Android UI refuse privileged actions when the server reports a viewer session. This reduces scope but is **not multiuser identity**, session rotation, per-file access control, native SSO, device pairing or a complete substitute for GoreeShield privacy and Wardveil threat controls.


## Upload framing and interrupted transfers

A selected-file upload must not produce an encrypted snapshot from a truncated request body. The API now requires the received byte count to match a nonnegative HTTP Content-Length, even if an upstream transport incorrectly reports a clean EOF. It distinguishes a true `http.MaxBytesError` (413, size limit) from an interrupted transport read (400), rather than treating any failure at the limit as an oversized file. In each handled failure, the temporary plaintext staging directory is removed and no Kopia snapshot command is started. New isolated Go regressions cover midstream read failure, declared-length mismatch, unknown-length streams larger than 32 MiB, empty content and staging cleanup. A hard process kill or server power loss can bypass deferred cleanup; startup/runtime orphan checks and operator review remain necessary. These tests do not certify automatic retry or background upload recovery.


## Viewer response data minimization

The coarse read-only token must not receive all raw upstream Kopia metadata merely because it can query a read endpoint. Viewer status omits repository storage endpoints and local connection details. Viewer snapshot listings are reconstructed using an explicit allowlist of ID, timestamp and an abbreviated source-folder leaf; hostnames, operating-system usernames, full source paths, root object identifiers and arbitrary tags are excluded. This is a defense against unnecessary disclosure to a read-only dashboard, **not** a promise of complete anonymity: snapshot leaf labels, timestamps, IDs and configured Rclone remote names remain visible and may be sensitive. The privileged administrative token is required for the original full metadata response and all mutations. The server never treats a client-supplied role header as authoritative.


## Private local snapshot-list client

`backupctl list` authenticates using the existing 0600 token file and requests metadata over numeric-loopback HTTP only; redirecting requests or oversized responses fail closed. It never outputs full source paths, repository object roots, usernames, hostnames, tags or bearer tokens to stdout. Source labels and snapshot IDs may still contain sensitive information, so console histories and terminal capture must remain protected. The command does not read archived file data or request paid provider quota information. Both administrator and scoped viewer tokens may use it.


## Credential-pattern source guard

Required Verify CI uses `scripts/check_credential_literals.py` to reject **new** tracked-text occurrences resembling complete AWS AKIA/ASIA access-key IDs or GitHub classic/fine-grained PATs. It compares each detection's path, category, opaque SHA-256 fingerprint and occurrence count against an explicitly retained baseline of 26 previously imported Rclone documentation/example matches, whose real validity is *not* certified. Reports and tests never print suspect matched bytes. A baseline entry is not permission to add new copies, does not validate ownership or legitimacy, and is not an exemption for other secret types. The baseline must only change through separately reviewed source changes and provenance checks; there is no auto-approve option. GitHub secret scanning, dependency review, security audits and real credential-owner notification remain independent requirements. Historical GitHub secret-scanning alert #1 remains **open** with unknown validity because current-tree test-fixture sanitization does not erase old commits or revoke credentials.

## Selected-file staging directory identity

The server records the private ingest directory's original filesystem identity at startup. For each upload it opens a directory-scoped `os.Root` handle, checks the opened identity against both the original inode and current configured path, and refuses replacement or symlink aliasing. Temporary file writes and cleanup occur under that pinned handle, not by resolving a caller-controlled absolute path. Staging cleanup removes one expected regular file and its matching child directory rather than recursively deleting an arbitrary location; unexpected contents or substituted children cause an error requiring operator inspection. Regression tests use disposable directories to rename and replace roots and prove unrelated paths remain unmodified.

**Known limit:** The Kopia external CLI still resolves an absolute staging path independently, between a final identity check and its filesystem operations; a malicious same-UID actor able to rename parent directories during that window remains outside the fully isolated threat model. The service must run under its own unprivileged UID with locked-down parent directories and disk encryption. A hard process crash may leave plaintext requiring review under the existing fail-closed orphan policy.

## SFTP test transport isolation

The `smoke-rclone-sftp-isolated.sh` drill uses only a 127.0.0.1 listener, randomly generated ephemeral host and password credentials, local private 0600 configuration files and disposable encrypted archives. It runs Rclone with an empty environment and explicit test config/cache rather than reading user or CI cloud remotes. A mismatched SSH host key must fail closed before any transfer. The test downloads the archived repository, compares per-file SHA-256 digests with the original encrypted repository, shuts down SFTP and independently verifies/restores from a new Kopia configuration. Cleanup kills the SSH process and deletes only the test-owned temporary directory. It does **not** prove a real server's host-key trust, MFA, remote authorization, provider durability, interrupted-transfer retries, rollback, or live repository consistency. Never copy a single production repository during concurrent updates without a validated provider-specific quiesce/checkpoint strategy.


## Metadata and hardlink restore acceptance boundary

Manual CI performs a synthetic local Kopia snapshot/100%-file-read/restore against UNIX restrictive file permissions (`0600`, `0750`, `0640`), a private directory (`0700`), a known modification time, Unicode content and a relative symlink. It compares restored bytes, modes, timestamps and link targets to the disposable source. The fixture also creates two file paths sharing one original inode. In the tested pinned Kopia build, both file byte streams were restored successfully but **their inode-sharing relationship was not preserved**. The script reports this as a known limitation and never describes a successful byte comparison as hardlink identity recovery. If an application depends on shared inode semantics, this implementation is not yet accepted for that workload without a dedicated operator-verified recovery procedure. POSIX ACLs, file owners, hardlink semantics across other filesystems, xattrs, sparse-file blocks, device files, SELinux labels and application-consistent database backups remain release-gate tasks.

## Synthetic S3 protocol test limits

S3 credentials are synthetic, generated at runtime, and the server binds exclusively to 127.0.0.1. It uses a disposable local directory and an isolated private Rclone configuration. A deliberately incorrect SigV4 secret must fail before any transfer. After a round-trip, SHA-256 of every encrypted repository object is compared; the server is stopped before independent Kopia verification and restore. HTTP here is **loopback only**, not a test of TLS over a network. Rclone's S3 server is experimental, and no real S3-compatible provider, bucket policy, account credential, object locking, interrupted upload, retention operation or live repository consistency is covered. A test pass does not certify real disaster recovery.

## Source-built binary evidence limitations

The optional manual CI publishes a deterministic, unsigned manifest of built Kopia and Rclone executable hashes, compiled Go dependency versions/sums and selected build flags, source-file SHA-256 and commit identity. It does not expose workstation paths or tokens. Go's compiled metadata can show trimpath but does not serialize the exact buildvcs flag; the source build script is separately hashed as reviewable intent, not proof that every command in that script was executed. The manifest is not a signed attestation and cannot establish independent reproducibility, provenance trust beyond the test runner, dependency license safety, complete runtime linkage or immunity to a compromised build runner. Production release work still requires independent builds, attestations, key custody, advisories, vulnerability triage and signed distribution.
