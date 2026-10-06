#!/usr/bin/env bash
set -euo pipefail

# Run from any directory. These are offline transport tests, not live-provider
# certification, CLI/materializer acceptance, or native Windows runtime tests.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
go_bin="${GO_BIN:-go}"
out="${RCC_STORAGE_VERIFICATION_DIR:-$root/tmp/object-storage-verification}"
mkdir -p "$out"
export GOMAXPROCS="${GOMAXPROCS:-2}"
export GOCACHE="${GOCACHE:-$out/go-cache}"
export GOPATH="${GOPATH:-$out/go-path}"
export ROBOCORP_HOME="$out/rcc-home"

if [[ ! -f blobs/assets/depxtraction.py || ! -f blobs/assets/micromamba.linux_amd64.gz ]]; then
  echo "Prepare the canonical embedded assets first: python -m invoke assets" >&2
  exit 1
fi

"$go_bin" version
CGO_ENABLED=0 "$go_bin" test -p 2 ./artifactprovider -count=1 | tee "$out/focused.log"
CGO_ENABLED=1 "$go_bin" test -p 2 -race ./artifactprovider -count=1 | tee "$out/race.log"
CGO_ENABLED=0 "$go_bin" vet -p 2 ./artifactprovider | tee "$out/vet.log"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$go_bin" test -p 2 -c ./artifactprovider -o "$out/artifactprovider-windows.test.exe"
# Repository-wide tests are a separate gate. Some unchanged legacy tests make
# real outbound requests, so this offline transport script does not run them.
git diff --check
