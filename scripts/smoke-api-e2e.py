#!/usr/bin/env python3
"""Opt-in API-to-engine acceptance using disposable local files only.

Requires trusted Kopia and Rclone executables. Creates an ephemeral local
encrypted Kopia repository and a loopback management service, then removes all
generated data. Never uses configured cloud remotes or a real backup repository.
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

PROJECT_ROOT = Path(__file__).resolve().parents[1]


def main():
    kopia = shutil.which(os.environ.get("KOPIA_BIN", "kopia"))
    rclone = shutil.which("rclone")
    if not kopia or not rclone:
        raise SystemExit("SKIP: trusted Kopia and Rclone CLI executables are required")
    # Avoid hidden home directories and /tmp: Snap Rclone may not access them.
    with tempfile.TemporaryDirectory(prefix="goreecloud-backup-api-smoke-", dir=Path.home()) as folder:
        work = Path(folder)
        (work / "bin").mkdir()
        source = work / "source"
        (source / "nested").mkdir(parents=True)
        (source / "important.txt").write_text("GoreeCloud Backup API acceptance\n")
        (source / "nested" / "second.txt").write_text("Nested disposable data\n")
        wrapper = work / "bin" / "kopia"
        wrapper.write_text(
            "#!/bin/sh\nexec " + shlex.quote(kopia) +
            " --config-file=" + shlex.quote(str(work / "kopia-config.json")) +
            ' "$@"\n'
        )
        wrapper.chmod(0o700)
        environment = os.environ.copy()
        environment["KOPIA_PASSWORD"] = secrets.token_hex(32)
        environment["GOREECLOUD_BACKUP_TOKEN"] = secrets.token_urlsafe(32)
        environment["RCLONE_CONFIG"] = os.devnull
        environment["PATH"] = str(work / "bin") + os.pathsep + environment.get("PATH", "")
        repo = subprocess.run(
            [str(wrapper), "repository", "create", "filesystem",
             "--path=" + str(work / "repository"),
             "--cache-directory=" + str(work / "cache"),
             "--no-check-for-updates"],
            env=environment, text=True, capture_output=True, timeout=90,
        )
        if repo.returncode != 0:
            raise RuntimeError("isolated repository creation failed: " + repo.stderr[-400:])

        binary = os.environ.get("GOREECLOUD_BACKUP_BIN")
        if not binary:
            binary = str(work / "goreecloud-backup")
            subprocess.run(
                ["go", "build", "-o", binary, "./cmd/goreecloud-backup"],
                cwd=PROJECT_ROOT, check=True, timeout=120,
            )

        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        environment["GOREECLOUD_BACKUP_ADDR"] = "127.0.0.1:" + str(port)
        endpoint = "http://127.0.0.1:" + str(port) + "/api/v1/"
        server = subprocess.Popen(
            [binary], env=environment, stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        def call(method, suffix, data=None, authenticated=True):
            headers = {"Content-Type": "application/json"}
            if authenticated:
                headers["Authorization"] = "Bearer " + environment["GOREECLOUD_BACKUP_TOKEN"]
            request = urllib.request.Request(
                endpoint + suffix,
                data=json.dumps(data).encode() if data is not None else None,
                headers=headers, method=method,
            )
            with urllib.request.urlopen(request, timeout=10) as response:
                return response.status, json.load(response)

        def reject(expected, method, suffix, data=None, authenticated=True):
            try:
                call(method, suffix, data, authenticated=authenticated)
            except urllib.error.HTTPError as error:
                assert error.code == expected, (error.code, expected)
                return
            raise AssertionError("request unexpectedly succeeded")

        def job(operation, **kwargs):
            status, submitted = call("POST", "jobs", {"type": operation, **kwargs})
            assert status == 202, (operation, submitted)
            for _ in range(150):
                time.sleep(0.2)
                _, current = call("GET", "jobs/" + submitted["id"])
                if current["status"] == "completed":
                    return current
                if current["status"] == "failed":
                    raise RuntimeError(operation + " failed: " + current.get("error", ""))
            raise TimeoutError(operation)

        try:
            for _ in range(80):
                try:
                    _, health = call("GET", "health")
                    break
                except urllib.error.URLError:
                    time.sleep(0.15)
            else:
                raise RuntimeError("loopback management API did not start")

            assert health["engines"]["kopia"] and health["engines"]["rclone"]
            reject(401, "GET", "health", authenticated=False)
            job("backup", source=str(source))
            # Verify the real encrypted repository through the authenticated API.
            job("verify-structure")
            reject(422, "POST", "jobs", {"type": "verify-full"})
            job("verify-full", confirm="VERIFY ALL BACKUPS")
            snapshots = job("snapshots")
            items = json.loads(snapshots["output"])
            assert items, "no snapshots"
            root_id = items[0]["rootEntry"]["obj"]
            restore = work / "restored"
            job("restore", snapshot_id=root_id, destination=str(restore), confirm="RESTORE")
            for relative in ("important.txt", "nested/second.txt"):
                assert (restore / relative).read_bytes() == (source / relative).read_bytes()

            destination = work / "destination"
            destination.mkdir()
            job("copy", source=str(source), destination=str(destination))
            job("check", source=str(source), destination=str(destination))
            (destination / "extra.txt").write_text("Disposable destination-only data")
            reject(422, "POST", "jobs", {
                "type": "sync", "source": str(source), "destination": str(destination),
            })
            job("sync-preview", source=str(source), destination=str(destination))
            assert (destination / "extra.txt").exists(), "dry-run deleted a file"
            job(
                "sync", source=str(source), destination=str(destination),
                confirm="DELETE DESTINATION EXTRAS",
            )
            assert not (destination / "extra.txt").exists()
            reject(409, "POST", "jobs", {
                "type": "sync", "source": str(source), "destination": str(destination),
                "confirm": "DELETE DESTINATION EXTRAS",
            })
            job("check", source=str(source), destination=str(destination))
            print("PASS: authenticated API + Kopia backup/structure/full verification/restore + Rclone copy/check/preview/sync")
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)


if __name__ == "__main__":
    main()
