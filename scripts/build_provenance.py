#!/usr/bin/env python3
"""Generate local, unsigned evidence for source-built Kopia/Rclone binaries.

This records actual compiled Go modules from go version -m, source file hashes,
and Git commit identity. It is not a cryptographic attestation or proof of
independent reproducibility, licensing, or vulnerability-free dependencies.
"""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
ENGINES = {"kopia": "github.com/kopia/kopia", "rclone": "github.com/rclone/rclone"}
SUM_PATTERN = re.compile(r"^h1:[A-Za-z0-9+/]{43}=$")
MODULE_PATTERN = re.compile(r"^[A-Za-z0-9!_.+~/-]+$")
VERSION_PATTERN = re.compile(r"^[A-Za-z0-9.+!_~-]+$")
COMMIT_PATTERN = re.compile(r"^[0-9a-f]{40}$")
TOOLCHAIN_PATTERN = re.compile(r"^go\d+\.\d+(?:\.\d+)?(?:[A-Za-z0-9.]*)$")

def sha256_file(file: Path) -> str:
    digest=hashlib.sha256()
    with file.open("rb") as stream:
        for chunk in iter(lambda:stream.read(1024*1024),b""):
            digest.update(chunk)
    return digest.hexdigest()

def parse_build_info(output: str, expected_module: str) -> dict:
    """Extract only known harmless Go metadata; never retain local file paths."""
    path=main=toolchain=None
    dependencies={}
    settings={}
    previous=None
    for raw_line in output.splitlines():
        if raw_line.startswith("\t"):
            fields=raw_line.strip().split("\t")
            if not fields: continue
            kind=fields[0]
            if kind=="path" and len(fields)==2:
                path=fields[1]
            elif kind=="mod" and len(fields)>=3:
                main=fields[1]
                previous=None
            elif kind=="dep" and len(fields)>=3:
                name,version=fields[1:3]
                if not MODULE_PATTERN.fullmatch(name) or not VERSION_PATTERN.fullmatch(version):
                    raise ValueError("Invalid compiled dependency metadata")
                checksum=fields[3] if len(fields)>=4 else None
                if checksum and not SUM_PATTERN.fullmatch(checksum):
                    raise ValueError("Invalid compiled dependency sum")
                if name in dependencies and dependencies[name]["version"]!=version:
                    raise ValueError("Conflicting compiled module versions")
                dependencies[name]={"module":name,"version":version}
                if checksum:dependencies[name]["sum"]=checksum
                previous=name
            elif kind=="=>" and previous:
                if len(fields)<2 or not MODULE_PATTERN.fullmatch(fields[1]):
                    raise ValueError("Invalid compiled module replacement")
                dependencies[previous]["replacement"]=fields[1]
                if len(fields)>=3:dependencies[previous]["replacementVersion"]=fields[2]
            elif kind=="build" and len(fields)>=2:
                key,separator,value=fields[1].partition("=")
                if separator and key in {"GOOS","GOARCH","CGO_ENABLED","GOAMD64","-trimpath","-buildvcs"}:
                    if any(ord(c)<32 for c in value) or len(value)>100:
                        raise ValueError("Invalid build setting")
                    settings[key]=value
            else:
                previous=None
        elif ": " in raw_line and "\t" not in raw_line:
            # Ignore the leading binary path; retain only Go toolchain suffix.
            candidate=raw_line.rsplit(": ",1)[-1]
            if TOOLCHAIN_PATTERN.fullmatch(candidate):toolchain=candidate
    if path!=expected_module or main!=expected_module or not toolchain:
        raise ValueError("Compiled binary identity or toolchain is unexpected")
    if not dependencies:
        raise ValueError("No compiled dependency metadata present")
    # The Go linker records -trimpath, but does not serialize the requested
    # -buildvcs=false value. Pin the reviewed build script hash separately.
    if settings.get("-trimpath")!="true":
        raise ValueError("Compiled artifact is missing -trimpath metadata")
    return {
        "module":expected_module,
        "goToolchain":toolchain,
        "settings":dict(sorted(settings.items())),
        "compiledModules":[dependencies[name] for name in sorted(dependencies)],
    }

def provenance_for_engine(engine: str)->dict:
    if engine not in ENGINES:raise ValueError("Unknown source engine")
    binary=ROOT/"build"/"bin"/engine
    if not binary.is_file() or binary.stat().st_size==0:
        raise ValueError("Source-built engine binary is missing")
    result=subprocess.run(
        ["go","version","-m",str(binary)],cwd=ROOT,
        capture_output=True,text=True,timeout=30,check=False,
    )
    if result.returncode:raise RuntimeError("Cannot read compiled binary metadata")
    info=parse_build_info(result.stdout,ENGINES[engine])
    source=ROOT/"third_party"/engine
    info["engine"]=engine
    info["binarySha256"]=sha256_file(binary)
    info["sourceGoModSha256"]=sha256_file(source/"go.mod")
    info["sourceGoSumSha256"]=sha256_file(source/"go.sum")
    info["compiledModuleCount"]=len(info["compiledModules"])
    return info

def build_provenance()->dict:
    process=subprocess.run(
        ["git","rev-parse","HEAD"],cwd=ROOT,
        capture_output=True,text=True,timeout=15,check=False
    )
    commit=process.stdout.strip()
    if process.returncode or not COMMIT_PATTERN.fullmatch(commit):
        raise RuntimeError("Current Git source commit is unavailable")
    return {
        "schemaVersion":1,
        "name":"GoreeCloud Backup source-built engine evidence",
        "gitCommit":commit,
        "upstreamSourceManifestSha256":sha256_file(ROOT/"third_party"/"sources.sha256"),
        "reviewedBuildScriptSha256":sha256_file(ROOT/"scripts"/"build-upstreams.sh"),
        "limitations":[
            "No cryptographic signing or independent artifact attestation",
            "Not a license audit or security advisory assessment",
            "One build does not establish independent byte-for-byte reproducibility",
        ],
        "engines":[provenance_for_engine(engine) for engine in ENGINES],
    }

def main()->int:
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output",default="build/provenance/engines.json")
    parser.add_argument("--check",action="store_true",help="compare manifest with current local compiled artifacts")
    options=parser.parse_args()
    output=(ROOT/options.output).resolve()
    if not output.is_relative_to(ROOT/"build"):
        parser.error("Output must be beneath ignored build/ directory")
    try:
        evidence=build_provenance()
        generated=json.dumps(evidence,ensure_ascii=False,indent=2,sort_keys=True)+"\n"
        if options.check:
            if not output.is_file() or output.read_text(encoding="utf-8")!=generated:
                raise ValueError("Build evidence does not match current source/binaries")
            print("PASS: compiled module and source hashes match recorded build evidence")
        else:
            output.parent.mkdir(parents=True,exist_ok=True)
            output.write_text(generated,encoding="utf-8")
            print("Wrote unsigned compiled-binary evidence for both pinned engines")
        return 0
    except (OSError,RuntimeError,ValueError,subprocess.TimeoutExpired) as exc:
        print(f"Build evidence generation failed: {type(exc).__name__}",file=sys.stderr)
        return 1

if __name__=="__main__":
    raise SystemExit(main())
