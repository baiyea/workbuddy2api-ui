#!/usr/bin/env bash
set -eu

if [ "$#" -gt 1 ]; then
  echo "usage: $0 [repository-root]" >&2
  exit 2
fi
if [ "$#" -eq 1 ]; then
  candidate="$(cd "$1" && pwd)"
else
  candidate="$(cd "$(dirname "$0")/.." && pwd)"
fi
scratch="$(mktemp -d "${TMPDIR:-/tmp}/wb2a-check.XXXXXX")"
prepared="$scratch/core"
cleanup() { rm -rf -- "$scratch"; }
trap cleanup EXIT

python3 "$candidate/scripts/overlay.py" prepare --output "$prepared"
go -C "$prepared" test ./...
go -C "$prepared" vet ./...
go -C "$prepared" test -race ./internal/auth ./internal/pool ./internal/upstream ./internal/scheduler ./internal/taskrun ./internal/bridge
go -C "$candidate/console" test -race ./...
node --test "$candidate/console/web_test.cjs"
python3 -m unittest discover -s "$prepared/scripts" -p 'test_*.py'
