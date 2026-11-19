#!/usr/bin/env bash
# Compile the pinned, included upstream sources; no remote installation scripts.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
command -v go >/dev/null || { echo "Install Go >= 1.26 before building bundled upstream engines" >&2; exit 1; }
command -v sha256sum >/dev/null || { echo "sha256sum required" >&2; exit 1; }
(cd "$ROOT" && sha256sum -c --status third_party/sources.sha256) || {
  echo "Pinned upstream files do not match reviewed hashes" >&2
  exit 1
}
mkdir -p "$ROOT/build/bin"
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org
export GOSUMDB=sum.golang.org
export GOFLAGS=-mod=readonly
for engine in kopia rclone; do
  echo "Building $engine from bundled source (modules remain separately pinned)"
  (cd "$ROOT/third_party/$engine" && go build -trimpath -buildvcs=false -o "$ROOT/build/bin/$engine" .)
  "$ROOT/build/bin/$engine" version 2>&1 | head -5 || "$ROOT/build/bin/$engine" --version
done
echo "Built both engines in build/bin; perform a restore drill before operational use"
