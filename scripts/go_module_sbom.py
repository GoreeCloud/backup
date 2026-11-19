#!/usr/bin/env python3
"""Generate a selected-Go-module CycloneDX inventory from pinned source trees.

No upstream code is executed by this script. It asks the trusted Go toolchain
for its resolved module list. This is NOT a binary dependency, license, or
vulnerability audit.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import quote

ROOT = Path(__file__).resolve().parent.parent
ENGINES = ("kopia", "rclone")


def objects_from_stream(content: str) -> list[dict]:
    decoder = json.JSONDecoder()
    index = 0
    result = []
    while index < len(content):
        while index < len(content) and content[index].isspace():
            index += 1
        if index >= len(content):
            break
        item, end = decoder.raw_decode(content, index)
        if not isinstance(item, dict):
            raise ValueError("Go returned a non-object module record")
        if item.get("Error"):
            raise ValueError("Go did not resolve a module: " + str(item.get("Path", "<unknown>")))
        if not isinstance(item.get("Path"), str) or not item["Path"]:
            raise ValueError("Go returned a module without a path")
        result.append(item)
        index = end
    if not result:
        raise ValueError("Go returned no module records")
    return result


def package_url(name: str, version: str) -> str:
    path = quote(name, safe="/.-_")
    return "pkg:golang/" + path + ("@" + quote(version, safe=".-_+") if version else "")


def build_inventory(module_sets: dict[str, list[dict]]) -> dict:
    collected: dict[tuple[str, str], dict] = {}
    for engine, modules in module_sets.items():
        if engine not in ENGINES:
            raise ValueError("Unexpected upstream engine")
        for mod in modules:
            name = mod["Path"]
            if not isinstance(name, str) or not name:
                raise ValueError("Invalid module path")
            actual = mod.get("Replace", {})
            version = actual.get("Version", mod.get("Version", ""))
            if not isinstance(version, str):
                raise ValueError("Invalid Go module version")
            key = (name, version)
            if key not in collected:
                collected[key] = {"engines": set(), "replacement": set()}
            collected[key]["engines"].add(engine)
            if actual:
                target = actual.get("Path", "")
                if target:
                    collected[key]["replacement"].add(str(target))
    components = []
    for (name, version), properties in sorted(collected.items()):
        record = {
            "type": "library",
            "bom-ref": package_url(name, version),
            "name": name,
            "purl": package_url(name, version),
            "properties": [
                {"name": "goreecloud:resolved-engine", "value": engine}
                for engine in sorted(properties["engines"])
            ],
        }
        if version:
            record["version"] = version
        if name in ("github.com/kopia/kopia", "github.com/rclone/rclone"):
            record["type"] = "application"
        record["properties"].extend(
            {"name": "goreecloud:replacement-target", "value": target}
            for target in sorted(properties["replacement"])
        )
        components.append(record)
    return {
        "bomFormat": "CycloneDX",
        "specVersion": "1.6",
        "version": 1,
        "metadata": {
            "component": {"type": "application", "name": "GoreeCloud Backup", "version": "0.1.0-dev"},
            "properties": [
                {"name": "goreecloud:inventory-scope", "value": "resolved go list -m all module selections"},
                {"name": "goreecloud:limitations", "value": "not a license, advisory or compiled-binary SBOM audit"},
            ],
        },
        "components": components,
    }


def get_modules(engine: str) -> list[dict]:
    source = ROOT / "third_party" / engine
    if not (source / "go.mod").is_file():
        raise ValueError("Pinned upstream module source is unavailable")
    env = os.environ.copy()
    env["GOTOOLCHAIN"] = "local"
    result = subprocess.run(
        ["go", "list", "-m", "-json", "all"],
        cwd=source,
        env=env,
        capture_output=True,
        text=True,
        timeout=180,
        check=False,
    )
    if result.returncode:
        # Go errors may include private environment data. Do not echo raw
        # stderr; instead fail closed with source-engine context only.
        raise RuntimeError(f"{engine}: Go module resolution failed (exit {result.returncode})")
    return objects_from_stream(result.stdout)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default="build/sbom/go-modules.cdx.json")
    args = parser.parse_args()
    output = (ROOT / args.output).resolve()
    if not output.is_relative_to(ROOT / "build"):
        parser.error("SBOM output must be beneath the ignored build/ directory")
    try:
        records = {engine: get_modules(engine) for engine in ENGINES}
        inventory = build_inventory(records)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(inventory, ensure_ascii=False, sort_keys=True, indent=2) + "\n", encoding="utf-8")
        print(f"Wrote {len(inventory['components'])} resolved Go module records to {output.relative_to(ROOT)}")
        return 0
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as exc:
        print(f"Inventory generation failed: {type(exc).__name__}; no incomplete inventory was published", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
