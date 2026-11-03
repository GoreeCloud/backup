# Job cancellation — development alpha

GoreeCloud Backup supports **best-effort cancellation** of queued and running operations. Cancellation does not guarantee rollback or removal of partially created files.

## API

With the existing bearer token:

```http
POST /api/v1/jobs/{id}/cancel
Authorization: Bearer <management-token>
```

- **202 Accepted:** cancellation requested; check final status.
- **401 Unauthorized:** token missing or invalid.
- **403 Forbidden:** prohibited cross-origin write.
- **404 Not Found:** unknown or expired job.
- **409 Conflict:** job already finished.

The dashboard shows **Cancel operation** for queued/running jobs. Status is `cancelling` until acknowledged, then `cancelled`. A process which completes successfully immediately before cancellation may remain `completed`.

## Safety and limitations

- A queued cancellation prevents the queued command from running.
- A running cancellation signals the command context; an already-running engine may have changed data.
- Interrupted Rclone synchronization can leave partial changes; it is not an undo operation. Review destinations and independent backups.
- Interrupted Kopia snapshot/restore requires repository and restored-data verification before recovery is considered complete.
- The job-status journal, when explicitly enabled, persists final statuses without storing file paths, engine output or credentials. If the service restarts during cancellation, that unfinished job is marked **interrupted**, not completed.
- Per-process job concurrency stays limited to one running operation and 12 admitted jobs, with a 30-minute per-job timeout including queue wait.

Unit/race tests cover authentication, queued/running cancellation, replay and persistence, and subprocess termination. Large-data live-engine interruption and a real-browser accessibility audit have not been verified.
