#!/usr/bin/env python3
"""Opt-in, isolated real-Kopia test of the GoreeCloud Backup snapshot trigger.

No cloud remotes, no production repositories, no services installed/enabled.
Requires a separately acquired and independently trusted Kopia executable.
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
        prefix="goreecloud-backup-trigger-smoke-", dir=Path.home()
    ) as tempdir:
        work = Path(tempdir)
        (work / "source").mkdir()
        (work / "bin").mkdir()
        (work / "cache").mkdir()
        (work / "source" / "example.txt").write_text(
            "GoreeCloud disposable post-backup verification test\n"
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
        trigger_bin = work / "trigger"
        require_success(
            run(["go", "build", "-o", str(server_bin), "./cmd/goreecloud-backup"],
                env=env, timeout=150),
            "service build",
        )
        require_success(
            run(["go", "build", "-o", str(trigger_bin), "./cmd/goreecloud-backup-trigger"],
                env=env, timeout=150),
            "trigger build",
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

        def trigger(*options):
            return run(
                [str(trigger_bin), "-source=" + str(work / "source"),
                 "-token-file=" + str(token_file), f"-address=127.0.0.1:{port}",
                 "-max-wait=2m", *options],
                env=env, timeout=150,
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

            a = require_success(trigger("-verify-after=structure"),
                                "snapshot + structural verification")
            assert a.stdout.count("job: ") == 2, "missing verification job ID"
            before = len(jobs())
            unconfirmed = trigger("-verify-after=full")
            assert unconfirmed.returncode != 0, "unacknowledged full check allowed"
            assert "requires explicit cost acknowledgment" in unconfirmed.stderr
            assert len(jobs()) == before, "invalid mode created an API job"

            b = require_success(
                trigger("-verify-after=full", "-acknowledge-full-verification-cost"),
                "snapshot + full repository verification",
            )
            assert b.stdout.count("job: ") == 2, "missing full verification job ID"

            entries = jobs()
            summary = {}
            for entry in entries:
                key = (entry["type"], entry["status"])
                summary[key] = summary.get(key, 0) + 1
            expected = {
                ("backup", "completed"): 2,
                ("verify-structure", "completed"): 1,
                ("verify-full", "completed"): 1,
            }
            if summary != expected:
                raise RuntimeError(f"unexpected isolated job statuses: {summary}")

            # A private journal must not store the token or backed-up paths.
            persisted = (work / "state" / "jobs.json").read_text()
            assert token not in persisted, "token leaked to journal"
            assert str(work / "source") not in persisted, "source path leaked to journal"
            print("PASS: isolated snapshot trigger + real Kopia structure/full checks")
            print("PASS: invalid full mode rejected without any API job")
            print("PASS: four completed API jobs and private status-only journal")
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)


if __name__ == "__main__":
    main()
