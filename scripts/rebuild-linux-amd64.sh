#!/usr/bin/env bash
# Two fresh runners execute independently; compare SHA-256 values only.
# No writes outside ignored build/ and no installation scripts.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
  echo "This acceptance lane requires Linux x86_64" >&2
  exit 1
fi
for tool in go sha256sum; do
  command -v "$tool" >/dev/null || { echo "Missing build tool: $tool" >&2; exit 1; }
done
expected_go="${GCB_REPRO_GO_VERSION:-go1.26.0}"
actual_go="$(go version | awk '{print $3}')"
if [[ "$actual_go" != "$expected_go" ]]; then
  echo "Incorrect Go toolchain version for reproducibility comparison" >&2
  exit 1
fi
(cd "$ROOT" && sha256sum -c --status third_party/sources.sha256) || {
  echo "Pinned upstream source SHA-256 mismatch" >&2
  exit 1
}
output="$ROOT/build/reproducibility"
mkdir -p "$output/bin"
export GOTOOLCHAIN=local
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0
export GOFLAGS=-mod=readonly
export GOPROXY=https://proxy.golang.org
export GOSUMDB=sum.golang.org
# Prevent comparison from reusing an existing compiled cache between jobs.
export GOCACHE="$output/isolated-cache"
mkdir -p "$GOCACHE"
for engine in kopia rclone; do
  echo "Building pinned $engine on isolated Linux AMD64 worker"
  (cd "$ROOT/third_party/$engine" &&
     go build -trimpath -buildvcs=false -o "$output/bin/$engine" .)
  binary="$output/bin/$engine"
  test -s "$binary"
  info="$(go version -m "$binary")"
  if ! grep -Fq $'\tbuild\tCGO_ENABLED=0' <<< "$info" ||
     ! grep -Fq $'\tbuild\tGOARCH=amd64' <<< "$info" ||
     ! grep -Fq $'\tbuild\tGOOS=linux' <<< "$info" ||
     ! grep -Fq $'\tbuild\t-trimpath=true' <<< "$info"; then
    echo "Compiled $engine does not match expected reproducibility metadata" >&2
    exit 1
  fi
done
for engine in kopia rclone; do
  hash="$(sha256sum "$output/bin/$engine" | cut -d' ' -f1)"
  if [[ ! "$hash" =~ ^[a-f0-9]{64}$ ]]; then
    echo "Failed to calculate a valid $engine hash" >&2
    exit 1
  fi
  printf '%s  %s\n' "$hash" "$engine"
done
echo "PASS: both source engines compiled on this Linux AMD64 worker; compare hashes with another independent worker"
