# Security policy and limitations

**Development only. Do not use as an externally exposed production recovery service.**

- Startup requires `GCB_API_TOKEN` with 32 or more bytes. Generate one from a cryptographically secure random source and provide it through a protected secret mechanism, never Git or HTTP URLs.
- Default binding is `127.0.0.1:8787`. Android requires trusted HTTPS. Use a narrowly scoped TLS proxy, auth/rate-limit controls and/or a private VPN; no reverse proxy is bundled.
- Token is held in in-memory React and Android state. Neither client persists it. Do not enter tokens into untrusted pages or transmit them across plaintext networks.
- Go never invokes a shell; external commands are fixed-argument Kopia/Rclone commands with execution timeouts and bounded standard output. Do not put hostile wrappers in the executable search path.
- The API is single-admin and lacks per-user accounts, authorization roles, rate limits, full audit logs and key rotation workflow. Integration with GoreeCloud Identity and Wardveil remains planned.
- The source-root containment check resolves symlinks but does not prevent concurrent filesystem mutation (TOCTOU). Keep the tree immutable to untrusted users for the duration of a backup.
- Real restore integrity, retention deletion safety, disaster recovery and cloud backend compatibility are **unverified**. Test isolated backups and restores before trusting any repository.
- No telemetry or trackers are intentionally included. GoreeShield-specific consent/retention policies and Mesh data exchange require separate control verification.

For private vulnerability reports, use GitHub's private vulnerability reporting if it is enabled on this repository; otherwise contact the maintainer through a secure established channel. Do not publish exploit details, access tokens or private storage contents in issues.

## Threat-model priorities

1. Stolen bearer token / unrestricted admin API; mitigate through TLS, least access, secret rotation and future Identity integration.
2. Repository secret compromise; isolate service user, file permissions, separate secure recovery material.
3. Data corruption, incomplete backup or undecryptable archive; require proof-of-restore, checksums and audited retention operations.
4. Untrusted Rclone remote or upstream dependency; verify versions, provenance, licenses and advisories; pin audited release artifacts.
5. Arbitrary filesystem access via symlink races; constrain source ownership and implement stronger native filesystem containment before multi-user exposure.
