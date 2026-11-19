# Documented local patches to attributed upstream sources

## 2026-11-19 — Kopia expired-STS test fixture hardening

- **Original upstream:** kopia/kopia v0.23.1, commit `72ec08fd8edb86c67ed27099bf1b955e1f308ffa`.
- **Changed path:** `third_party/kopia/repo/blob/s3/s3_storage_test.go`.
- **Change:** In `customProvider.RetrieveWithCredContext`, when `forceExpired` is true, replace a credential-shaped test access-key ID and paired secret-key literal with deliberately invalid `GCB_EXPIRED_TEST_*` placeholders. Keep the expired-session-token test flow and all other source untouched.
- **Reason:** GitHub secret-scanning alert #1 identified the original AWS-style temporary access-key identifier from the source import, with validity unknown. Its upstream test context suggests an intentionally expired credential fixture, not authorization to assume the value is invalid. Do not publish or replicate the original credential value here.
- **Build and behavioral risk:** Only a test helper is changed, but the behavior of the MinIO STS test must be revalidated with an isolated test server; compile verification alone is not equivalent to an integration run.
- **Historical disclosure:** This source-only fix does **not** delete historical GitHub commits or revoke credentials. Retain the alert and obtain authoritative AWS/provider evidence if it is to be resolved.
- **Upgrade procedure:** Rebase this single test-only change onto a newly verified upstream tag, review its source diff and dependencies, recompute `third_party/sources.sha256`, rerun full tests and record results.

The official upstream copyright and license terms remain in force.
