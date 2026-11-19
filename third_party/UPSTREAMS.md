# Pinned upstream source — GoreeCloud Backup

This repository includes source snapshots of Kopia and Rclone **inside the owning repository**, preserving upstream copyright, license, package boundaries, build metadata, assets and notices. The Kopia tree includes one documented security-fixture delta; see [PATCHES.md](PATCHES.md). They are not independently authored GoreeCloud code and remain separate Go modules.

| Component | Source path | Upstream | Tag | Verified upstream commit | License |
| --- | --- | --- | --- | --- | --- |
| Kopia | `third_party/kopia/` | https://github.com/kopia/kopia | `v0.23.1` | `72ec08fd8edb86c67ed27099bf1b955e1f308ffa` | Apache-2.0 — `third_party/kopia/LICENSE` |
| Rclone | `third_party/rclone/` | https://github.com/rclone/rclone | `v1.75.2` | `be8d6044eacb843b74d335db0c5fbe09222031fe` | MIT — `third_party/rclone/COPYING`; additional upstream notices retained |

## Provenance

The two official GitHub tags were fetched as shallow, read-only working trees via HTTPS with their upstream repository URLs and commit identifiers verified. Their tracked source was imported without Git history or `.git` internals. No upstream build scripts, installers, packaging scripts or network hooks were run during import. At import, `diff -qr` confirmed that all source matched the checked-out upstream tags (excluding `.git`). A subsequent *explicitly recorded* local patch modifies one Kopia test fixture; see PATCHES.md before claiming byte-identical upstream source. The standalone upstream `go.mod` and `go.sum` files were preserved.

`third_party/sources.sha256` records checksums for every imported ordinary file. Run `sha256sum -c third_party/sources.sha256` from the repository root to detect accidental changes. A hash manifest is a check of **internal file consistency**, not proof of upstream authenticity: compare release tags and SHA with verified upstream references before updating.

## Build and maintenance boundaries

Use `scripts/build-upstreams.sh` to compile pinned source with an explicitly installed supported Go toolchain, the public Go proxy, checksum database, and read-only module manifests. Build output is `build/bin/kopia` and `build/bin/rclone` and is ignored by Git. Go resolves many version-pinned transitive dependencies on demand; **not every transitive dependency's source is bundled**. Build reproducibility, full license/SBOM auditing, malware scanning and cross-platform binary acceptance remain separate gates.

Original upstream copyrights and trademarks remain with their owners. **Do not represent this source copy as an independent GoreeCloud implementation**. Any changes must record the original upstream base, modified paths, licenses, test results and update path in PATCHES.md. Avoid using unreviewed embedded upstream scripts. Source is imported for auditable integration and eventual vetted, first-party distribution, not as an endorsement of every upstream optional feature.

## Known limitations

- Installed system Kopia/Rclone binaries remain usable; binaries compiled here must be selected explicitly with `GCB_KOPIA_BIN` and `GCB_RCLONE_BIN` unless they are separately installed.
- Kopia's upstream Rclone repository storage backend is described upstream as experimental/not maintained; choose native Kopia backends for a reliability-first deployment.
- No automatic cloud sync, mobile file backup, production key management, restore UI or enterprise-grade role permissions exist merely by importing these engines.
- Source import has not yet completed a comprehensive third-party license audit or code-level threat model. Before stable release, run dependency/SBOM/advisory review and regression tests against both upstreams.
