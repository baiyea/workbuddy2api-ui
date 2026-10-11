#!/usr/bin/env bash
# Isolated final correction evidence runner; never a product startup entry point.
set -u
base=/var/lib/docker/volumes/wb2a-protocol-verify-f2de220763d9/_data
source="$base/final-fix-3f4e6be"
logs="$base/final-fix-3f4e6be-logs"
export TMPDIR="$base/tmp"
export GOCACHE="$base/gocache"
export GOPATH="$base/gopath"
mkdir -p "$TMPDIR" "$logs"
test ! -e "$source" || exit 90
mkdir "$source"
tar -xf /tmp/source.tar -C "$source" || exit 91
cd "$source" || exit 92
failures=0
run() {
  local name="$1"
  shift
  "$@" > "$logs/$name.log" 2>&1
  local result=$?
  printf '%s exit=%s\n' "$name" "$result" | tee -a "$logs/exits.log"
  if [ "$result" != 0 ]; then failures=1; fi
}
run versions bash -c 'go version; python --version; python -c "from importlib.metadata import version; print({x:version(x) for x in [\"anthropic\",\"openai\",\"httpx\"]})"'
run scripts python -m unittest discover -s scripts -p 'test_*.py' -v
run deploy python -m unittest discover -s deploy -p 'test_*.py' -v
run desktop python -m unittest discover -s desktop/scripts -p 'test_*.py' -v
run check bash scripts/check.sh
run compose docker compose --env-file /dev/null -f docker-compose.yml config --quiet
run anthropic-fixtures python scripts/check_anthropic_sdk.py
run server-race bash -c 'python scripts/overlay.py prepare --output .build/final-server-race && go -C .build/final-server-race test -race ./internal/server'
if [ "$failures" != 0 ]; then exit 1; fi
export WB2A_SDK_PYTHON=/usr/local/bin/python
export WB2A_RESPONSES_SDK_PYTHON=/usr/local/bin/python
export WB2A_ACCEPTANCE_CORE_IMAGE=wb2api-final-fix-3f4e6be-core
export WB2A_ACCEPTANCE_CONSOLE_IMAGE=wb2api-final-fix-3f4e6be-console
export WB2A_BUILD_HTTP_PROXY=http://host.docker.internal:7890
export WB2A_BUILD_HTTPS_PROXY=http://host.docker.internal:7890
run acceptance bash scripts/acceptance.sh
run images docker image inspect "$WB2A_ACCEPTANCE_CORE_IMAGE" "$WB2A_ACCEPTANCE_CONSOLE_IMAGE" --format '{{.Id}} {{.Os}}/{{.Architecture}}'
run patch-identity python scripts/overlay.py identity
exit "$failures"
