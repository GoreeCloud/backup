#!/usr/bin/env python3
"""Opt-in local Linux kill/restart simulation of the private UTC scheduler.

A new encrypted disposable repository is created; a trusted Kopia binary
backs it up. Only the disposable scheduler process is killed; the separate
loopback API server is expected to finish the original job. No timers,
production repositories, remote cloud accounts or host services are modified.
"""
import json
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
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def run(args, env, timeout=150):
    p = subprocess.run(args, cwd=ROOT, env=env, text=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if p.returncode:
        raise RuntimeError(f"command exited {p.returncode}: " + p.stderr[-300:])
    return p


def main():
    name = os.environ.get("KOPIA_BIN", "kopia")
    binary = shutil.which(name)
    if not binary or not Path(binary).is_file():
        raise SystemExit("SKIP: separately verified Kopia executable required")
    binary = str(Path(binary).resolve())
    with tempfile.TemporaryDirectory(
        prefix="goreecloud-backup-scheduler-crash-", dir=Path.home()
    ) as name:
        work = Path(name)
        (work / "source").mkdir()
        (work / "bin").mkdir()
        (work / "cache").mkdir()
        (work / "source" / "test.txt").write_text("Private one-shot recovery drill\n")

        token = secrets.token_urlsafe(40)
        token_file = work / "management-token"
        fd = os.open(token_file, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(fd, "w") as f:
            f.write(token + "\n")

        env = os.environ.copy()
        env["KOPIA_PASSWORD"] = secrets.token_hex(32)
        env["GOREECLOUD_BACKUP_TOKEN"] = token
        env["RCLONE_CONFIG"] = os.devnull
        env["PATH"] = str(work / "bin") + os.pathsep + env.get("PATH", "")

        config = work / "kopia-config.json"
        run(
            [binary, "--config-file=" + str(config),
             "repository", "create", "filesystem",
             "--path=" + str(work / "repository"),
             "--cache-directory=" + str(work / "cache"),
             "--no-check-for-updates"], env
        )
        wrapper = work / "bin" / "kopia"
        wrapper.write_text(
            "#!/bin/sh\nsleep 6\nexec " + shlex.quote(binary)
            + " --config-file=" + shlex.quote(str(config)) + ' "$@"\n'
        )
        wrapper.chmod(0o700)

        service_bin = work / "service"
        scheduler_bin = work / "scheduler"
        run(["go", "build", "-o", str(service_bin),
             "./cmd/goreecloud-backup"], env)
        run(["go", "build", "-o", str(scheduler_bin),
             "./cmd/goreecloud-backup-scheduler"], env)

        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env["GOREECLOUD_BACKUP_ADDR"] = f"127.0.0.1:{port}"
        env["GOREECLOUD_BACKUP_STATE_DIR"] = str(work / "jobs")
        base = f"http://127.0.0.1:{port}/api/v1/"

        # The due window includes the next eight minutes (UTC hour-aligned).
        offset = (datetime.now(timezone.utc).minute - 1) % 60
        policy_path = work / "policy.json"
        policy_path.write_text(json.dumps({
            "schema": 1,
            "policies": [{
                "id": "crash-simulation",
                "enabled": True,
                "source": str(work / "source"),
                "interval_minutes": 60,
                "offset_minutes": offset,
                "verify_after": "none",
                "acknowledge_full_verification_cost": False,
            }],
        }))
        policy_path.chmod(0o600)
        state_dir = work / "schedule-state"

        def api(route):
            req = urllib.request.Request(
                base + route, headers={"Authorization": "Bearer " + token}
            )
            with urllib.request.urlopen(req, timeout=10) as response:
                return json.load(response)

        def jobs():
            return api("jobs")["jobs"]

        schedule_cmd = [
            str(scheduler_bin), "-run-due", "-config=" + str(policy_path),
            "-state-dir=" + str(state_dir), "-token-file=" + str(token_file),
            f"-address=127.0.0.1:{port}", "-max-wait=2m",
        ]

        server = subprocess.Popen([str(service_bin)], env=env, cwd=ROOT,
                                  stdout=subprocess.DEVNULL,
                                  stderr=subprocess.DEVNULL)
        scheduler = None
        try:
            for _ in range(80):
                try:
                    api("health")
                    break
                except (OSError, urllib.error.URLError):
                    time.sleep(0.1)
            else:
                raise RuntimeError("local management API did not become ready")

            scheduler = subprocess.Popen(schedule_cmd, env=env, cwd=ROOT,
                                         stdout=subprocess.DEVNULL,
                                         stderr=subprocess.DEVNULL)
            # Only kill when an actual API job has started and the durable
            # claim exists. This tests the uncertain in-flight case.
            for _ in range(150):
                time.sleep(0.1)
                statefile = state_dir / "schedule-state.json"
                records = {}
                if statefile.exists():
                    records = json.loads(statefile.read_text()).get("records", {})
                current_jobs = jobs()
                if records.get("crash-simulation", {}).get("status") == "claimed" and (
                    len(current_jobs) == 1 and current_jobs[0]["status"] == "running"
                ):
                    break
            else:
                raise RuntimeError("could not observe running job with durable claim")

            if scheduler.poll() is not None:
                raise RuntimeError("scheduler exited before simulated failure")
            scheduler.kill()
            scheduler.wait(timeout=5)
            restarted = run(schedule_cmd, env)
            if "No enabled policy due" not in restarted.stdout:
                raise AssertionError("restart did not skip uncertain claimed slot")
            if len(jobs()) != 1:
                raise AssertionError("restart submitted a duplicate backup")

            ledger = json.loads((state_dir / "schedule-state.json").read_text())
            record = ledger["records"]["crash-simulation"]
            if record["status"] != "interrupted":
                raise AssertionError("in-flight claim not marked interrupted")

            for _ in range(150):
                time.sleep(0.2)
                original = jobs()[0]
                if original["status"] in ("completed", "failed", "interrupted", "cancelled"):
                    break
            else:
                raise TimeoutError("original API job remained active too long")

            if original["status"] != "completed":
                raise AssertionError("original engine job did not complete independently")
            if len(jobs()) != 1:
                raise AssertionError("API received another job after restart")

            persisted = (state_dir / "schedule-state.json").read_text()
            journal = (work / "jobs" / "jobs.json").read_text()
            for text in (persisted, journal):
                if token in text or str(work / "source") in text:
                    raise AssertionError("private ledger leaked secret/source")
            print("PASS: killed scheduler after durable claim and running API job")
            print("PASS: restart marked uncertain claim interrupted without duplication")
            print("PASS: original real Kopia snapshot finished independently")
            print("PASS: private ledger/journal contain no token or source paths")
        finally:
            if scheduler is not None and scheduler.poll() is None:
                scheduler.kill()
                scheduler.wait(timeout=5)
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)


if __name__ == "__main__":
    main()
