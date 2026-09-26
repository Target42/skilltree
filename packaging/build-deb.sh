#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mkdir -p dist
GOOS=linux GOARCH=amd64 go build -o skilltree ./cmd/skilltree
docker run --rm -v "$ROOT":/work -w /work goreleaser/nfpm package \
  --config packaging/nfpm.yaml --packager deb --target dist/
echo "deb written to dist/"
