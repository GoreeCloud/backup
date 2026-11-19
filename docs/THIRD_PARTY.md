# Third-party provenance and license notices

## Included upstream source and runtime integrations

- **Kopia** — https://github.com/kopia/kopia — Apache License 2.0. Supplies encryption, snapshotting, deduplication and repository engines when separately installed. Copyright belongs to upstream contributors. Pinned source is included in `third_party/kopia/` (tag v0.23.1, commit 72ec08fd8edb86c67ed27099bf1b955e1f308ffa), with original license.
- **Rclone** — https://github.com/rclone/rclone — MIT license (see upstream COPYING). Supplies optional cloud remote configuration and names when separately installed; Kopia's upstream Rclone provider is experimental/not maintained. Pinned source is included in `third_party/rclone/` (tag v1.75.2, commit be8d6044eacb843b74d335db0c5fbe09222031fe), with its original COPYING and bundled notices.

Pinned tag commits and source file checksums are recorded in `third_party/UPSTREAMS.md` and `third_party/sources.sha256`. No compiled binaries are committed or certified. Operators can build both with `scripts/build-upstreams.sh` and choose the outputs explicitly; they remain separate executable processes, not rebranded GoreeCloud source. The import is not a full transitive license/SBOM/advisory audit.

## Bundled first-party ecosystem code

- **GoreeCloud UI** — https://github.com/GoreeCloud/gc-ui — MIT. `web/src/gc-tokens.css` is a sourced copy of its generated stylesheet. See its own LICENSE and canonical token registry. Copying this stylesheet does not make its UI component library a dependency or imply product visual acceptance.

## Front-end and Android dependencies

React, Vite, TypeScript, Gradle Wrapper (Apache-2.0), Kotlin and AndroidX dependencies remain upstream dependencies resolved by their respective package managers, governed by their original licenses. A full dependency SBOM, advisories, notarization and provenance verification are required before release. This is not an upstream-complete licensing audit.

GoreeCloud's first-party Go service, React screens and Kotlin client source are licensed under the repository-level Apache-2.0 license; upstream notices are not superseded.

## Local upstream difference

One Kopia S3 integration-test credential fixture was changed from a credential-shaped literal to unmistakably invalid development placeholders. It is not a production credential configuration or a change to the backup encryption engine. The original publicly committed test value may remain in repository history; do not assume that a current file edit invalidates or revokes leaked credentials. See `third_party/PATCHES.md` and GitHub secret-scanning alert #1.
