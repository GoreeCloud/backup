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
