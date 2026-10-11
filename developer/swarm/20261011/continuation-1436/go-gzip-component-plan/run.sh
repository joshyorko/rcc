#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if [[ "${RCC125_GO_GZIP_ADMISSION:-}" != "root-admitted-go-gzip-component-v1" ]]; then
  echo "execution blocked: explicit root admission required" >&2
  exit 64
fi
GO=/workspace/work/rcc-swarm/tools/go1.26.9/bin/go
export GOTOOLCHAIN=local
export GOCACHE=/home/agent/.cache/go-build
export GOTMPDIR="$PWD/.go-tmp"
mkdir -p "$GOTMPDIR"
trap 'rm -rf -- "$GOTMPDIR"' EXIT
free_bytes=$(stat -f -c '%a %S' . | awk '{print $1 * $2}')
if (( free_bytes < 2688548864 )); then
  echo "execution blocked: workspace free bytes $free_bytes below floor+4MiB" >&2
  exit 65
fi
allocated=$(du -s -B1 . | awk '{print $1}')
if (( allocated >= 1048576 )); then
  echo "execution blocked: owned data already at/over 1 MiB cap ($allocated)" >&2
  exit 66
fi
"$GO" version
exec timeout --signal=TERM --kill-after=5s 120s "$GO" run ./main.go --plan ./plan.json
