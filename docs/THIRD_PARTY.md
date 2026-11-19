# Third-party provenance and license notices

## Runtime integrations (not vendored)

- **Kopia** — https://github.com/kopia/kopia — Apache License 2.0. Supplies encryption, snapshotting, deduplication and repository engines when separately installed. Copyright belongs to upstream contributors. Source is not copied into this first revision.
- **Rclone** — https://github.com/rclone/rclone — MIT license (see upstream COPYING). Supplies optional cloud remote configuration and names when separately installed; Kopia's upstream Rclone provider is experimental/not maintained.

No exact Kopia/Rclone binaries, release hashes or package versions are bundled or certified; operators must verify their own. These remain external processes, not rebranded source.

## Bundled first-party ecosystem code

- **GoreeCloud UI** — https://github.com/GoreeCloud/gc-ui — MIT. `web/src/gc-tokens.css` is a sourced copy of its generated stylesheet. See its own LICENSE and canonical token registry. Copying this stylesheet does not make its UI component library a dependency or imply product visual acceptance.

## Front-end and Android dependencies

React, Vite, TypeScript, Gradle Wrapper (Apache-2.0), Kotlin and AndroidX dependencies remain upstream dependencies resolved by their respective package managers, governed by their original licenses. A full dependency SBOM, advisories, notarization and provenance verification are required before release. This is not an upstream-complete licensing audit.

GoreeCloud's first-party Go service, React screens and Kotlin client source are licensed under the repository-level Apache-2.0 license; upstream notices are not superseded.
