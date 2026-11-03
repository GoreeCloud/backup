# Backup integrity verification — development alpha

GoreeCloud Backup provides two **manual, authenticated** Kopia integrity-check jobs for the currently connected repository.

| Operation | Engine command | What it proves |
| --- | --- | --- |
| Verify backup structure | `kopia snapshot verify --verify-files-percent=0 --file-parallelism=2` | Walks snapshot structures and checks referenced repository objects |
| Verify all backup file contents | `kopia snapshot verify --verify-files-percent=100 --file-parallelism=2` | Also downloads, decrypts and decompresses every backed-up file for verification |

The complete check **requires typed confirmation**: `VERIFY ALL BACKUPS`. It is read-oriented and does not intentionally delete or repair repository objects, but it can use significant CPU, disk cache, network bandwidth, and billed cloud egress. The existing 30-minute per-job deadline still applies. Large repositories may need an operator-initiated external Kopia verification process with a longer controlled deadline.

A successful verification is **not** proof that a particular application or operating system can be restored successfully. Continue performing actual test restores and independent checksum comparisons. A failed, cancelled, interrupted or timed-out verification must never be displayed or treated as passed.

Results appear in the job history. The current dashboard displays bounded engine output, not a dedicated verification report; optional private journal persistence records job **status only** and omits filenames and engine output. No unattended recurring verification scheduler, immutable storage policy, automatic corruption repair or production recovery guarantee is claimed.

Official Kopia documentation:
- https://kopia.io/docs/advanced/consistency/
- https://kopia.io/docs/reference/command-line/common/snapshot-verify/
