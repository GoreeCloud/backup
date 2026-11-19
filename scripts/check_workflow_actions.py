#!/usr/bin/env python3
"""Reject mutable third-party GitHub Action references in repository workflows.

Only local relative actions (./...) or external owner/repo@40-hex-sha actions
are allowed. This inspection does not execute or install any action.
"""
from __future__ import annotations

from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parent.parent
WORKFLOWS = ROOT / ".github" / "workflows"
USES_LINE = re.compile(r"^\s*(?:-\s+)?uses:\s*([^\s#]+)")
PINNED = re.compile(r"^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+@[a-f0-9]{40}$")


def examine_workflow(source: str) -> tuple[int, list[str]]:
    """Return external action count and diagnosed immutable-ref violations."""
    checked = 0
    errors: list[str] = []
    for lineno, line in enumerate(source.splitlines(), start=1):
        match = USES_LINE.match(line)
        if not match:
            continue
        ref = match.group(1)
        if ref.startswith("./"):
            continue
        checked += 1
        if not PINNED.fullmatch(ref):
            errors.append(f"line {lineno}: action must use a full immutable commit SHA")
    return checked, errors


def main() -> int:
    paths = sorted(WORKFLOWS.glob("*.yml")) + sorted(WORKFLOWS.glob("*.yaml"))
    if not paths:
        print("No GitHub Actions workflow files found", file=sys.stderr)
        return 1
    total = 0
    failed = False
    for path in paths:
        checked, errors = examine_workflow(path.read_text(encoding="utf-8"))
        total += checked
        for issue in errors:
            print(f"{path.relative_to(ROOT)}: {issue}", file=sys.stderr)
            failed = True
    if total == 0:
        print("No external workflow actions found", file=sys.stderr)
        return 1
    if failed:
        return 1
    print(f"PASS: {total} external workflow action references use immutable commit SHAs")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
