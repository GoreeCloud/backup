#!/usr/bin/env python3
"""Opt-in acceptance: restore from an independently copied encrypted Kopia repo.

Uses only disposable local directories as a stand-in for offsite storage.
Requires a separately trusted Kopia executable. No remote provider,
cloud credentials, existing repositories, or production service is accessed.
"""
import hashlib
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


def execute(args, env, title, *, timeout=150, succeed=True):
    result = subprocess.run(
        args, env=env, cwd=ROOT, capture_output=True, text=True, timeout=timeout
    )
    if succeed and result.returncode:
        # Never print process environment (repository password and API token).
        raise RuntimeError(f"{title}: exit {result.returncode}; " + result.stderr[-320:])
    return result


def digest_tree(root):
    result = {}
    for item in sorted(root.rglob("*")):
        if item.is_file():
            result[item.relative_to(root).as_posix()] = hashlib.sha256(item.read_bytes()).hexdigest()
    return result


def free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def wrapper(root, name, binary, config):
    folder = root / name
    folder.mkdir(mode=0o700)
    script = folder / "kopia"
    script.write_text(
        "#!/bin/sh\nexec " + shlex.quote(str(binary)) +
        " --config-file=" + shlex.quote(str(config)) + ' "$@"\n'
    )
    script.chmod(0o700)
    return script


class LocalAPI:
    def __init__(self, executable, env, work):
        self.token = secrets.token_urlsafe(40)
        self.port = free_port()
        self.env = env.copy()
        self.env["GOREECLOUD_BACKUP_TOKEN"] = self.token
        self.env["GOREECLOUD_BACKUP_ADDR"] = f"127.0.0.1:{self.port}"
        self.env["GOREECLOUD_BACKUP_STATE_DIR"] = str(work)
        self.url = f"http://127.0.0.1:{self.port}/api/v1/"
        self.proc = subprocess.Popen(
            [str(executable)], cwd=ROOT, env=self.env,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
        )

    def request(self, method, endpoint, data=None, authenticate=True):
        headers = {"Content-Type": "application/json"}
        if authenticate:
            headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(
            self.url + endpoint,
            data=json.dumps(data).encode() if data is not None else None,
            method=method, headers=headers
        )
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, json.load(response)

    def ready(self):
        for _ in range(80):
            try:
                self.request("GET", "health")
                return
            except urllib.error.URLError:
                time.sleep(0.15)
        raise RuntimeError("isolated local management API did not start")

    def job(self, action, **kwargs):
        status, value = self.request("POST", "jobs", {"type": action, **kwargs})
        if status != 202:
            raise AssertionError(f"{action} was not admitted")
        for _ in range(150):
            time.sleep(0.2)
            _, current = self.request("GET", "jobs/" + value["id"])
            if current["status"] in ("completed", "failed", "cancelled", "interrupted"):
                if current["status"] != "completed":
                    raise RuntimeError(f"{action} ended as {current['status']}")
                return current
        raise TimeoutError(action)

    def stop(self):
        self.proc.terminate()
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)


def main():
    kopia_name = os.environ.get("KOPIA_BIN", "kopia")
    kopia = shutil.which(kopia_name)
    if not kopia or not Path(kopia).is_file():
        raise SystemExit("SKIP: separately verified Kopia binary required")
    kopia = Path(kopia).resolve()

    with tempfile.TemporaryDirectory(prefix="goreecloud-backup-copy-recovery-", dir=Path.home()) as folder:
        work = Path(folder)
        source = work / "source"
        (source / "nested").mkdir(parents=True)
        (source / "important.txt").write_text("Private GoreeCloud disposable offsite test\n")
        (source / "nested" / "blob.bin").write_bytes(os.urandom(65536))
        (source / "nested" / "notes.txt").write_text("Encrypted, copied, reconnected, restored\n")
        original_digests = digest_tree(source)
        assert len(original_digests) == 3

        env = os.environ.copy()
        env["KOPIA_PASSWORD"] = secrets.token_hex(32)
        env.pop("GOREECLOUD_BACKUP_TOKEN", None)
        config1 = work / "connected-source.json"
        config2 = work / "connected-mirror.json"
        cli1 = wrapper(work, "source-bin", kopia, config1)
        cli2 = wrapper(work, "mirror-bin", kopia, config2)
        env1 = env.copy()
        env1["PATH"] = str(cli1.parent) + os.pathsep + env.get("PATH", "")
        env2 = env.copy()
        env2["PATH"] = str(cli2.parent) + os.pathsep + env.get("PATH", "")
        orig_repo = work / "original-repository"
        copied_repo = work / "offsite-copy"
        execute(
            [str(cli1), "repository", "create", "filesystem",
             "--path=" + str(orig_repo), "--cache-directory=" + str(work / "source-cache"),
             "--no-check-for-updates"], env1, "create disposable encrypted repository"
        )
        executable = work / "goreecloud-backup"
        execute(["go", "build", "-o", str(executable), "./cmd/goreecloud-backup"],
                env1, "compile actual GoreeCloud Backup API")

        first = LocalAPI(executable, env1, work / "first-job-state")
        try:
            first.ready()
            first.job("backup", source=str(source))
            first.job("verify-structure")
            first.job("verify-full", confirm="VERIFY ALL BACKUPS")
            snapshot_response = first.job("snapshots")
            snapshots = json.loads(snapshot_response["output"])
            assert snapshots and "rootEntry" in snapshots[0]
            snapshot_id = snapshots[0]["rootEntry"]["obj"]
        finally:
            first.stop()

        # Create an independent local encrypted object copy, WITHOUT invoking
        # Rclone or accessing configured remote targets. This deliberately
        # tests restore portability; it does not validate cloud transport.
        shutil.copytree(orig_repo, copied_repo, symlinks=False)
        assert digest_tree(orig_repo) == digest_tree(copied_repo), "repository copy differs"

        # Hide the original data and configuration *inside the temporary test*
        # to prove that the new connection is not reusing the original.
        orig_repo.rename(work / "original-repository-unavailable")
        source.rename(work / "source-unavailable")
        config1.rename(work / "original-config-unavailable.json")
        assert not orig_repo.exists() and not source.exists()

        wrong_env = env2.copy()
        wrong_env["KOPIA_PASSWORD"] = secrets.token_hex(32)
        bad = execute(
            [str(kopia), "--config-file=" + str(work / "bad-key.json"),
             "repository", "connect", "filesystem", "--path=" + str(copied_repo),
             "--cache-directory=" + str(work / "wrong-key-cache"),
             "--no-check-for-updates"], wrong_env,
            "reject wrong repository password", succeed=False
        )
        assert bad.returncode != 0, "encrypted repository accepted wrong password"

        execute(
            [str(cli2), "repository", "connect", "filesystem",
             "--path=" + str(copied_repo),
             "--cache-directory=" + str(work / "fresh-restore-cache"),
             "--no-check-for-updates"], env2, "reconnect copied encrypted repository"
        )
        second = LocalAPI(executable, env2, work / "second-job-state")
        try:
            second.ready()
            # The freshly started recovery API must still require its new
            # management token; repository connectivity is not authorization.
            try:
                second.request("GET", "health", authenticate=False)
            except urllib.error.HTTPError as error:
                assert error.code == 401, error.code
            else:
                raise AssertionError("recovery API did not require authentication")
            second.job("verify-structure")
            second.job("verify-full", confirm="VERIFY ALL BACKUPS")
            restored = work / "restored-from-copy"
            second.job("restore", snapshot_id=snapshot_id,
                       destination=str(restored), confirm="RESTORE")
            assert digest_tree(restored) == original_digests, "recovered files differ"
            # A repeated restore into an occupied destination must be
            # rejected before creating a job or modifying restored files.
            try:
                second.request("POST", "jobs", {
                    "type": "restore", "snapshot_id": snapshot_id,
                    "destination": str(restored), "confirm": "RESTORE",
                })
            except urllib.error.HTTPError as error:
                assert error.code == 422, error.code
            else:
                raise AssertionError("occupied restore destination was accepted")
            assert digest_tree(restored) == original_digests
            for state in (work / "first-job-state" / "jobs.json",
                          work / "second-job-state" / "jobs.json"):
                journal = state.read_text()
                assert second.token not in journal and first.token not in journal
                assert str(source) not in journal
            print("PASS: encrypted repository independently copied and SHA-256 checked")
            print("PASS: original repository/source/config unavailable during reconnect")
            print("PASS: wrong repository password rejected")
            print("PASS: fresh configuration/cache, structural and full verification")
            print("PASS: recovered API rejected missing auth and occupied restore paths")
            print("PASS: actual authenticated Backup API restored byte-identical files")
            print("PASS: job histories exclude API tokens and original source paths")
        finally:
            second.stop()


if __name__ == "__main__":
    main()
