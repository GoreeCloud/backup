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

The native recovery companion rejects URL paths, userinfo, query strings, fragments and non-HTTPS endpoints; it does not follow redirects with bearer credentials, limits JSON responses to 1 MiB and keeps credentials in volatile process memory. Android backup/restore of phone files, Keystore-based account pairing and audited background jobs are not implemented. The app excludes its window from screenshots and recent-apps thumbnails by default. The optional server-side verification and contained-restore actions require explicit in-app consent and remain governed by backend validation and capacity limits.

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

## Ephemeral GitHub runner user-namespace permission

The corrected recovery CI probe proved that GitHub's Ubuntu 24.04 hosted runner denied Bubblewrap's UID-map setup (`bwrap: setting up uid map: Permission denied`). For the **manual, synthetic recovery workflow only**, a dedicated step records the runner's original `kernel.apparmor_restrict_unprivileged_userns` value and temporarily sets it to 0 *after* source builds and the corruption drill; an `if: always()` cleanup step restores the exact original 0/1 value, including after test failures. This is not a production host tuning recommendation and is not used by any deployed GoreeCloud service. The result must be revalidated on GitHub before claiming that the hosted restore test passed. This change acknowledges a temporary reduction in isolation on the disposable runner to exercise the independent Bubblewrap mount boundary; it must not be copied onto personal or shared hosts.
