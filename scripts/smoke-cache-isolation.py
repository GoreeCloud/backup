#!/usr/bin/env python3
"""Opt-in negative integrity drill, corrupting only a new disposable repository.

Requires a separately trusted Kopia binary (KOPIA_BIN). Does not touch any
configured production repository, cloud remotes, or existing user files.
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


def launch(args, env, timeout=120):
    return subprocess.run(args, cwd=ROOT, env=env, text=True,
                          capture_output=True, timeout=timeout)


def must_succeed(args, env, operation):
    result = launch(args, env)
    if result.returncode:
        raise RuntimeError(f"{operation} failed (code {result.returncode}): "
                           + result.stderr[-250:])
    return result


def main():
    kopia_name = os.environ.get("KOPIA_BIN", "kopia")
    kopia = shutil.which(kopia_name)
    if not kopia or not Path(kopia).is_file():
        raise SystemExit("SKIP: a separately verified Kopia binary is required")

    with tempfile.TemporaryDirectory(prefix="goreecloud-backup-corruption-", dir=Path.home()) as tmp:
        work = Path(tmp)
        source = work / "source"
        source.mkdir()
        # Uncompressible data makes an encrypted content-pack candidate large
        # and distinguishable from the small repository configuration files.
        (source / "test-blob.bin").write_bytes(os.urandom(2 * 1024 * 1024))
        (work / "bin").mkdir()
        (work / "cache").mkdir()
        wrapper = work / "bin" / "kopia"
        wrapper.write_text("#!/bin/sh\nexec " + shlex.quote(str(Path(kopia).resolve()))
                           + " --config-file=" + shlex.quote(str(work / "kopia-config.json"))
                           + ' "$@"\n')
        wrapper.chmod(0o700)
        token = secrets.token_urlsafe(40)
        env = os.environ.copy()
        env["KOPIA_PASSWORD"] = secrets.token_hex(32)
        env["GOREECLOUD_BACKUP_TOKEN"] = token
        env["RCLONE_CONFIG"] = os.devnull
        env["PATH"] = str(work / "bin") + os.pathsep + env.get("PATH", "")
        env["GOREECLOUD_BACKUP_STATE_DIR"] = str(work / "state")
        must_succeed(
            [str(wrapper), "repository", "create", "filesystem",
             "--path=" + str(work / "repository"),
             "--cache-directory=" + str(work / "cache"),
             "--no-check-for-updates"],
            env, "new encrypted test repository",
        )
        binary = work / "backup-service"
        must_succeed(["go", "build", "-o", str(binary), "./cmd/goreecloud-backup"],
                     env, "server build")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env["GOREECLOUD_BACKUP_ADDR"] = f"127.0.0.1:{port}"
        base = f"http://127.0.0.1:{port}/api/v1/"
        server = subprocess.Popen([str(binary)], cwd=ROOT, env=env,
                                  stdout=subprocess.DEVNULL,
                                  stderr=subprocess.DEVNULL)

        def api(method, route, payload=None):
            req = urllib.request.Request(
                base + route,
                data=json.dumps(payload).encode() if payload is not None else None,
                headers={"Authorization": "Bearer " + token,
                         "Content-Type": "application/json"},
                method=method,
            )
            with urllib.request.urlopen(req, timeout=10) as response:
                return response.status, json.load(response)

        def wait_job(job_type, **kwargs):
            status, admitted = api("POST", "jobs", {"type": job_type, **kwargs})
            if status != 202:
                raise AssertionError("job not admitted: " + job_type)
            job_id = admitted["id"]
            for _ in range(150):
                time.sleep(0.2)
                _, current = api("GET", "jobs/" + job_id)
                if current["status"] in ("completed", "failed", "cancelled", "interrupted"):
                    return current
            raise TimeoutError("job failed to reach final status: " + job_type)

        try:
            for _ in range(80):
                try:
                    api("GET", "health")
                    break
                except (urllib.error.URLError, OSError):
                    time.sleep(0.1)
            else:
                raise RuntimeError("local API did not become available")

            initial = wait_job("backup", source=str(source))
            if initial["status"] != "completed":
                raise RuntimeError("disposable snapshot failed")

            prior = wait_job("verify-full", confirm="VERIFY ALL BACKUPS")
            if prior["status"] != "completed":
                raise RuntimeError("uncorrupted baseline repository verification failed")

            packs = sorted(
                (p for p in (work / "repository").rglob("*")
                 if p.is_file() and p.stat().st_size >= 1_000_000),
                key=lambda p: p.stat().st_size, reverse=True,
            )
            if not packs:
                raise RuntimeError("no disposable encrypted content pack found")
            blob = packs[0]
            # Only the largest encrypted blob in our just-created repository
            # is altered. All data and the temporary repository are removed
            # when this test exits.
            with blob.open("r+b") as stream:
                stream.seek(blob.stat().st_size // 2)
                previous = stream.read(1)
                if not previous:
                    raise RuntimeError("disposable blob unexpectedly empty")
                stream.seek(blob.stat().st_size // 2)
                stream.write(bytes([previous[0] ^ 0xFF]))
                stream.flush()
                os.fsync(stream.fileno())

            damaged = wait_job("verify-full", confirm="VERIFY ALL BACKUPS")
            if damaged["status"] != "failed":
                raise AssertionError(
                    "corrupted backing storage incorrectly accepted: " + damaged["status"]
                )
            if token in json.dumps(damaged) or str(source) in damaged.get("error", ""):
                raise AssertionError("sensitive values leaked to job error")
            if len(damaged.get("output", "")) > 12 * 1024:
                raise AssertionError("engine output exceeded API size bound")

            journal = (work / "state" / "jobs.json").read_text()
            if token in journal or str(source) in journal:
                raise AssertionError("journal retained token or source path")
            print("PASS: clean encrypted repository verification completed")
            print("PASS: corrupted disposable content pack detected through API")
            print("PASS: failed job status, bounded output and private journal")
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)


if __name__ == "__main__":
    main()
