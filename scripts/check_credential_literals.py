#!/usr/bin/env python3
"""Fail CI on newly added, high-confidence access-key-shaped text.

This is deliberately a *delta* check. Its path+fingerprint+count baseline
records previously imported, unverified upstream examples as opaque SHA256
digests. The scanner never displays suspected credential bytes. It does not
scan historical Git commits, test live validity, or replace GitHub's scanner.
"""
from __future__ import annotations

from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
BASELINE = ROOT / "security/credential-literal-baseline.json"
# High-confidence prefixed formats only. These deliberately omit generic
# password assignments (too noisy) and do not claim to catch every secret.
PATTERNS = {
    "aws_access_key_id": re.compile(rb"(?<![A-Z0-9])(?:AKIA|ASIA)[A-Z0-9]{16}(?![A-Z0-9])"),
    "github_classic_token": re.compile(rb"(?<![A-Za-z0-9])gh[pousr]_[A-Za-z0-9]{36}(?![A-Za-z0-9])"),
    "github_fine_grained_token": re.compile(rb"(?<![A-Za-z0-9])github_pat_[A-Za-z0-9_]{80,100}(?![A-Za-z0-9_])"),
}
MAX_TEXT_SIZE = 8 * 1024 * 1024


def scan_blob(path: str, content: bytes) -> Counter[tuple[str, str, str]]:
    """Return fingerprint counts only; raw matched values never leave here."""
    found: Counter[tuple[str, str, str]] = Counter()
    if b"\0" in content[:8192]:
        return found
    for kind, pattern in PATTERNS.items():
        for match in pattern.finditer(content):
            digest = hashlib.sha256(match.group()).hexdigest()
            found[(path, kind, digest)] += 1
    return found


def scan_repository() -> Counter[tuple[str, str, str]]:
    tracked = subprocess.check_output(
        ["git", "ls-files", "-z"], cwd=ROOT
    ).split(b"\0")
    results: Counter[tuple[str, str, str]] = Counter()
    for raw in tracked:
        if not raw:
            continue
        name = raw.decode("utf-8", "surrogateescape")
        file = ROOT / name
        if not file.is_file():
            raise RuntimeError("Tracked path is missing or not a file")
        # Skip known binary formats, but treat any abnormally large text file
        # as an error requiring review rather than silently omitting it.
        content = file.read_bytes()
        if len(content) > MAX_TEXT_SIZE and b"\0" not in content[:8192]:
            raise RuntimeError(f"Tracked text file exceeds scanner bound: {name}")
        results.update(scan_blob(name, content))
    return results


def load_baseline(path: Path = BASELINE) -> Counter[tuple[str, str, str]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("version") != 1 or not isinstance(payload.get("entries"), list):
        raise ValueError("Unexpected credential-pattern baseline format")
    baseline: Counter[tuple[str, str, str]] = Counter()
    for item in payload["entries"]:
        name = item["path"]
        kind = item["kind"]
        digest = item["sha256"]
        count = item["count"]
        if (not isinstance(name, str) or name.startswith("/") or ".." in Path(name).parts
            or kind not in PATTERNS or not isinstance(digest, str)
            or not re.fullmatch(r"[0-9a-f]{64}", digest)
            or type(count) is not int or count < 1):
            raise ValueError("Unsafe baseline entry")
        key = (name, kind, digest)
        if key in baseline:
            raise ValueError("Duplicate baseline entry")
        baseline[key] = count
    return baseline


def additions(
    actual: Counter[tuple[str, str, str]],
    expected: Counter[tuple[str, str, str]],
) -> list[tuple[str, str, int]]:
    result = []
    for (path, kind, digest), count in actual.items():
        excess = count - expected.get((path, kind, digest), 0)
        if excess > 0:
            result.append((path, kind, excess))
    return sorted(result)


def main() -> int:
    try:
        actual = scan_repository()
        baseline = load_baseline()
        unexpected = additions(actual, baseline)
    except (OSError, subprocess.CalledProcessError, ValueError, RuntimeError) as exc:
        print(f"Credential pattern scan unavailable: {exc}", file=sys.stderr)
        return 2
    if unexpected:
        print("New suspicious credential-shaped literals detected. No raw values shown.", file=sys.stderr)
        for path, kind, count in unexpected:
            print(f"  {path}: {kind} ({count} new occurrence(s))", file=sys.stderr)
        print("Review privately and remove risky values; do not blindly baseline new findings.", file=sys.stderr)
        return 1
    print(
        f"PASS: no new high-confidence AWS/GitHub key-shaped literals; "
        f"{sum(actual.values())} existing matched occurrence(s) require independent triage"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
