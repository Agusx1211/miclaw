#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/dist}"

mkdir -p "$OUT_DIR"
cd "$ROOT_DIR"

build() {
	local arch="$1"
	local out="$OUT_DIR/miclaw_linux_${arch}"
	echo "building $out"
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$out" ./cmd/miclaw
}

build amd64
build arm64

echo "done"
