#!/usr/bin/env python3
"""Opt-in isolated real-Kopia test of at-most-once Linux policy scheduling.

No cloud remotes, production repositories, or systemd services modified.
Requires a separately acquired and independently trusted Kopia executable.
"""
import json
from datetime import datetime, timezone
import os
import secrets
import shlex
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def run(args, *, env, timeout=150):
    completed = subprocess.run(
        args, cwd=ROOT, env=env, text=True, capture_output=True, timeout=timeout
    )
    return completed


def require_success(result, step):
    if result.returncode:
        raise RuntimeError(f"{step} failed (exit={result.returncode}): "
                           + result.stderr[-350:])
    return result


def main():
    binary_name = os.environ.get("KOPIA_BIN", "kopia")
    kopia = shutil.which(binary_name)
    if not kopia:
        raise SystemExit("SKIP: independently trusted Kopia binary required (KOPIA_BIN)")
    kopia = str(Path(kopia).resolve())
    if not Path(kopia).is_file():
        raise SystemExit("SKIP: Kopia binary path is not a regular file")

    # The selected test root is created with mode 0700 under the user's HOME.
    with tempfile.TemporaryDirectory(
        prefix="goreecloud-backup-scheduler-smoke-", dir=Path.home()
    ) as tempdir:
        work = Path(tempdir)
        (work / "source").mkdir()
        (work / "bin").mkdir()
        (work / "cache").mkdir()
        (work / "source" / "example.txt").write_text(
            "GoreeCloud disposable UTC schedule test\n"
        )

        token_file = work / "token"
        token = secrets.token_urlsafe(40)
        fd = os.open(token_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(token + "\n")

        wrapper = work / "bin" / "kopia"
        wrapper.write_text(
            "#!/bin/sh\nexec " + shlex.quote(kopia)
            + " --config-file=" + shlex.quote(str(work / "kopia-config.json"))
            + ' "$@"\n'
        )
        wrapper.chmod(0o700)

        env = os.environ.copy()
        env["KOPIA_PASSWORD"] = secrets.token_hex(32)
        env["GOREECLOUD_BACKUP_TOKEN"] = token
        env["RCLONE_CONFIG"] = os.devnull
        env["PATH"] = str(work / "bin") + os.pathsep + env.get("PATH", "")
        require_success(
            run(
                [
                    str(wrapper), "repository", "create", "filesystem",
                    "--path=" + str(work / "repository"),
                    "--cache-directory=" + str(work / "cache"),
                    "--no-check-for-updates",
                ],
                env=env,
                timeout=90,
            ),
            "isolated encrypted repository creation",
        )

        server_bin = work / "server"
        scheduler_bin = work / "scheduler"
        require_success(
            run(["go", "build", "-o", str(server_bin), "./cmd/goreecloud-backup"],
                env=env, timeout=150),
            "service build",
        )
        require_success(
            run(["go", "build", "-o", str(scheduler_bin), "./cmd/goreecloud-backup-scheduler"],
                env=env, timeout=150),
            "scheduler build",
        )

        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]
        env["GOREECLOUD_BACKUP_ADDR"] = f"127.0.0.1:{port}"
        env["GOREECLOUD_BACKUP_STATE_DIR"] = str(work / "state")
        base = f"http://127.0.0.1:{port}"
        server = subprocess.Popen([str(server_bin)], env=env, cwd=ROOT,
                                  stdout=subprocess.DEVNULL,
                                  stderr=subprocess.DEVNULL)

        def jobs():
            req = urllib.request.Request(base + "/api/v1/jobs",
                                         headers={"Authorization": "Bearer " + token})
            with urllib.request.urlopen(req, timeout=10) as response:
                return json.load(response)["jobs"]

        # Run inside an aligned UTC slot with a fresh policy file and ledger.
        # At 2 minutes after the slot start, the two policies have up to
        # 8 additional minutes in the 10-minute acceptance window.
        minute = datetime.now(timezone.utc).minute
        due_offset = (minute - 2) % 60
        missed_offset = (minute - 20) % 60
        policy_file = work / "schedules.json"
        policy_file.write_text(json.dumps({
            "schema": 1,
            "policies": [
                {"id": "alpha", "enabled": True, "source": str(work / "source"),
                 "interval_minutes": 60, "offset_minutes": due_offset,
                 "verify_after": "none", "acknowledge_full_verification_cost": False},
                {"id": "beta", "enabled": True, "source": str(work / "source"),
                 "interval_minutes": 60, "offset_minutes": due_offset,
                 "verify_after": "structure", "acknowledge_full_verification_cost": False},
                {"id": "disabled", "enabled": False, "source": str(work / "source"),
                 "interval_minutes": 60, "offset_minutes": due_offset,
                 "verify_after": "none", "acknowledge_full_verification_cost": False},
                {"id": "missed", "enabled": True, "source": str(work / "source"),
                 "interval_minutes": 60, "offset_minutes": missed_offset,
                 "verify_after": "none", "acknowledge_full_verification_cost": False}
            ]
        }))
        policy_file.chmod(0o600)
        ledger_dir = work / "scheduler-state"

        def schedule(*options):
            return run(
                [str(scheduler_bin), "-config=" + str(policy_file),
                 "-state-dir=" + str(ledger_dir), "-token-file=" + str(token_file),
                 f"-address=127.0.0.1:{port}", "-max-wait=2m", *options],
                env=env, timeout=180,
            )

        try:
            for _ in range(80):
                try:
                    request = urllib.request.Request(
                        base + "/api/v1/health",
                        headers={"Authorization": "Bearer " + token},
                    )
                    with urllib.request.urlopen(request, timeout=3) as response:
                        json.load(response)
                        break
                except (OSError, urllib.error.URLError):
                    time.sleep(0.1)
            else:
                raise RuntimeError("isolated local service did not start")

            check = require_success(schedule("-check-config"), "policy validation")
            assert "4 policies, 3 explicitly enabled" in check.stdout
            assert not ledger_dir.exists(), "config inspection created persistent state"
            assert len(jobs()) == 0, "config inspection started a backup"

            first = require_success(schedule("-run-due"), "first due UTC slot")
            assert "Policy alpha: completed" in first.stdout
            assert "Policy beta: completed" in first.stdout
            assert "disabled" not in first.stdout
            assert "missed" not in first.stdout
            entries = jobs()
            summary = {}
            for entry in entries:
                key = (entry["type"], entry["status"])
                summary[key] = summary.get(key, 0) + 1
            expected = {
                ("backup", "completed"): 2,
                ("verify-structure", "completed"): 1,
            }
            if summary != expected:
                raise RuntimeError(f"unexpected isolated job statuses: {summary}")

            # Separate process invocation simulates a normal service restart:
            # the durable state must prevent same-slot backup submission.
            repeated = require_success(schedule("-run-due"), "repeat one-shot")
            assert "No enabled policy due" in repeated.stdout
            assert len(jobs()) == 3, "second invocation duplicated a snapshot"

            persisted = (ledger_dir / "schedule-state.json").read_text()
            state = json.loads(persisted)
            assert state["schema"] == 1
            assert set(state["records"]) == {"alpha", "beta"}
            assert all(item["status"] == "completed"
                       for item in state["records"].values())
            assert token not in persisted, "token leaked to schedule state"
            assert str(work / "source") not in persisted, "source path leaked to state"
            assert (ledger_dir / "schedule-state.json").stat().st_mode & 0o077 == 0
            job_journal = (work / "state" / "jobs.json").read_text()
            assert token not in job_journal
            assert str(work / "source") not in job_journal
            print("PASS: validated private opt-in UTC policies and disabled/missed gating")
            print("PASS: two snapshots plus structural verification through real Kopia API")
            print("PASS: repeated one-shot invocation skipped the already-claimed slot")
            print("PASS: private state-only ledger and job journal exclude paths/tokens")
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)


if __name__ == "__main__":
    main()
