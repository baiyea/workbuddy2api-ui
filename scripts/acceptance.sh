#!/usr/bin/env bash
set -euo pipefail

repo_root="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
case "$repo_root" in
  /*) ;;
  *) echo "repo root must be absolute" >&2; exit 2 ;;
esac
for path in upstream.lock scripts/overlay.py deploy/core.Dockerfile deploy/console.Dockerfile deploy/compose.acceptance.yml deploy/default-config.json deploy/acceptance-config.json deploy/acceptance.env deploy/mock_upstream.py; do
  test -e "$repo_root/$path" || { echo "missing $repo_root/$path" >&2; exit 2; }
done

suffix="$(date +%s)-$$"
fresh_project="wb2api-task4-$suffix"
legacy_project="$fresh_project-legacy"
fresh_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
legacy_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
projects=("$fresh_project" "$legacy_project")
acceptance_passed=false
core_image="${WB2A_ACCEPTANCE_CORE_IMAGE:-${fresh_project}-core}"
console_image="${WB2A_ACCEPTANCE_CONSOLE_IMAGE:-${fresh_project}-console}"
expected_commit="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["commit"])' "$repo_root/upstream.lock")"
expected_identity="$(python3 "$repo_root/scripts/overlay.py" identity)"
cookie_file="${TMPDIR:-/tmp}/${legacy_project}.cookie"

compose_for() {
  local project="$1" port="$2"
  shift 2
  WB2A_BIND_ADDRESS=127.0.0.1 \
  WB2A_PORT="$port" \
  WB2A_AUTHS_VOLUME="${project}_auths" \
  WB2A_DATA_VOLUME="${project}_data" \
  WB2A_KEYS_VOLUME="${project}_keys" \
  WB2A_CONFIG_FILE="${WB2A_ACCEPTANCE_CONFIG_FILE:-$repo_root/deploy/default-config.json}" \
  WB2A_ACCEPTANCE_MOCK="$repo_root/deploy/mock_upstream.py" \
  WB2A_ACCEPTANCE_CORE_IMAGE="$core_image" \
  WB2A_ACCEPTANCE_CONSOLE_IMAGE="$console_image" \
  WB2A_ADMIN_KEY="${WB2A_ACCEPTANCE_ADMIN_KEY:-}" \
  WB2A_API_KEY="${WB2A_ACCEPTANCE_API_KEY:-}" \
  WB2A_PUBLIC_ORIGIN="http://127.0.0.1:$port" \
  docker compose --env-file "$repo_root/deploy/acceptance.env" --project-directory "$repo_root" -p "$project" \
    -f "$repo_root/docker-compose.yml" -f "$repo_root/deploy/compose.acceptance.yml" "$@"
}

build_images() {
  local project="$1" port="$2"
  if [[ -n "${HTTP_PROXY:-}" && -n "${HTTPS_PROXY:-}" ]]; then
    compose_for "$project" "$port" build \
      --build-arg "HTTP_PROXY=$HTTP_PROXY" --build-arg "HTTPS_PROXY=$HTTPS_PROXY"
  elif [[ -n "${HTTP_PROXY:-}" ]]; then
    compose_for "$project" "$port" build --build-arg "HTTP_PROXY=$HTTP_PROXY"
  elif [[ -n "${HTTPS_PROXY:-}" ]]; then
    compose_for "$project" "$port" build --build-arg "HTTPS_PROXY=$HTTPS_PROXY"
  else
    compose_for "$project" "$port" build
  fi
}

create_volumes() {
  local project="$1"
  for kind in auths data keys; do
    docker volume create --label "wb2a.acceptance=$project" "${project}_${kind}" >/dev/null
  done
}

wait_live() {
  local url="$1"
  for _ in {1..100}; do
    curl --noproxy '*' -fsS "$url" >/dev/null 2>&1 && return
    sleep 0.1
  done
  echo "timed out waiting for $url" >&2
  return 1
}

remove_project_volumes() {
  local project="$1"
  for kind in auths data keys; do
    local volume="${project}_${kind}" label
    label="$(docker volume inspect -f '{{index .Labels "wb2a.acceptance"}}' "$volume" 2>/dev/null || true)"
    [[ "$label" == "$project" ]] && docker volume rm "$volume" >/dev/null || true
  done
}

cleanup() {
  rm -f -- "$cookie_file"
  if [[ "$acceptance_passed" == "true" && "${WB2A_ACCEPTANCE_KEEP:-}" == "true" ]]; then
    echo "acceptance kept: http://127.0.0.1:$legacy_port/ (project $legacy_project)"
    return
  fi
  compose_for "$fresh_project" "$fresh_port" down --remove-orphans >/dev/null 2>&1 || true
  compose_for "$legacy_project" "$legacy_port" down --remove-orphans >/dev/null 2>&1 || true
  for project in "${projects[@]}"; do
    remove_project_volumes "$project"
  done
}
trap cleanup EXIT

create_volumes "$fresh_project"
build_images "$fresh_project" "$fresh_port"
python3 -m unittest discover -s "$repo_root/deploy" -p test_compose.py -v
missing_config="$repo_root/.build/acceptance-missing-$suffix.json"
test ! -e "$missing_config"
for invalid_config in "$missing_config" "$repo_root/deploy" "$repo_root/README.md"; do
  if WB2A_ACCEPTANCE_CONFIG_FILE="$invalid_config" compose_for "$fresh_project" "$fresh_port" run --rm --no-deps core; then
    echo "invalid selected config unexpectedly started" >&2
    exit 1
  fi
done
test ! -e "$missing_config"
compose_for "$fresh_project" "$fresh_port" up -d --wait --no-build
[[ "$(compose_for "$fresh_project" "$fresh_port" port console 7863)" == "127.0.0.1:$fresh_port" ]]
wait_live "http://127.0.0.1:$fresh_port/livez"
[[ "$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$fresh_port/healthz")" == "503" ]]
[[ "$(compose_for "$fresh_project" "$fresh_port" exec -T core id -u)" == "10001" ]]
[[ "$(compose_for "$fresh_project" "$fresh_port" exec -T console id -u)" == "10001" ]]

mock_id="$(compose_for "$fresh_project" "$fresh_port" ps -q mock)"
core_id="$(compose_for "$fresh_project" "$fresh_port" ps -q core)"
console_id="$(compose_for "$fresh_project" "$fresh_port" ps -q console)"
docker inspect "$mock_id" "$core_id" "$console_id" | python3 -c '
import json,sys
mock,core,console=json.load(sys.stdin)
assert not any((core["HostConfig"]["PortBindings"] or {}).values())
assert not any((mock["HostConfig"]["PortBindings"] or {}).values())
assert set(mock["NetworkSettings"]["Networks"]) == {sys.argv[1] + "_mock"}
assert set(core["NetworkSettings"]["Networks"]) == {sys.argv[1] + "_mock"}
assert set(console["NetworkSettings"]["Networks"]) == {sys.argv[1] + "_mock", sys.argv[1] + "_entry"}
mounts={m["Destination"]:m["RW"] for m in console["Mounts"]}
assert "/app/auths" not in mounts and "/app/data" not in mounts
assert mounts == {"/run/wb2a": False}
core_config=next(m for m in core["Mounts"] if m["Destination"] == "/app/config.json")
assert core_config["RW"] is False and core_config["Source"].endswith("/deploy/default-config.json")
' "$fresh_project"
compose_for "$fresh_project" "$fresh_port" exec -T core sh -eu -c '
  test "$(stat -c %a /run/wb2a)" = 700
  test "$(stat -c %a /run/wb2a/keys.json)" = 600
  key="$(python3 -c "import json; print(json.load(open(\"/run/wb2a/keys.json\"))[\"bridge_key\"])")"
  wget -qO- --header="Authorization: Bearer $key" http://127.0.0.1:7863/internal/v1/info
' | python3 -c '
import json,sys
info=json.load(sys.stdin)
assert info["protocol"] == 1
assert info["upstream_commit"] == sys.argv[1]
assert info["patch_identity"] == sys.argv[2]
' "$expected_commit" "$expected_identity"
fresh_digest="$(compose_for "$fresh_project" "$fresh_port" exec -T core sha256sum /run/wb2a/keys.json | cut -d' ' -f1)"
WB2A_ACCEPTANCE_ADMIN_KEY=cccccccccccccccccccccccccccccccc WB2A_ACCEPTANCE_API_KEY=short-override \
  compose_for "$fresh_project" "$fresh_port" up -d --force-recreate --wait --no-build
curl --noproxy '*' -fsS -H "Origin: http://127.0.0.1:$fresh_port" -H 'Content-Type: application/json' \
  --data '{"key":"cccccccccccccccccccccccccccccccc"}' "http://127.0.0.1:$fresh_port/admin/login" >/dev/null
[[ "$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer short-override' "http://127.0.0.1:$fresh_port/status")" == "200" ]]
[[ "$fresh_digest" == "$(compose_for "$fresh_project" "$fresh_port" exec -T core sha256sum /run/wb2a/keys.json | cut -d' ' -f1)" ]]
compose_for "$fresh_project" "$fresh_port" exec -T core sh -c 'printf "%s\n" mock-account > /app/auths/preserved.txt; printf "%s\n" mock-state > /app/data/preserved.txt'
build_images "$fresh_project" "$fresh_port"
compose_for "$fresh_project" "$fresh_port" up -d --force-recreate --wait --no-build
[[ "$fresh_digest" == "$(compose_for "$fresh_project" "$fresh_port" exec -T core sha256sum /run/wb2a/keys.json | cut -d' ' -f1)" ]]
compose_for "$fresh_project" "$fresh_port" exec -T core sh -c 'grep -qx mock-account /app/auths/preserved.txt && grep -qx mock-state /app/data/preserved.txt'
compose_for "$fresh_project" "$fresh_port" down --remove-orphans >/dev/null
remove_project_volumes "$fresh_project"

export WB2A_ACCEPTANCE_CONFIG_FILE="$repo_root/deploy/acceptance-config.json"
create_volumes "$legacy_project"
docker run --rm --network none \
  --mount "type=volume,src=${legacy_project}_auths,dst=/auths" \
  --mount "type=volume,src=${legacy_project}_data,dst=/data" \
  alpine:3.20 sh -eu -c '
    printf "%s\n" '\''{"auth":{"accessToken":"mock-token","refreshToken":"mock-refresh","expiresAt":4102444800,"domain":"www.workbuddy.ai","realm":"global"},"account":{"uid":"mock","enterpriseId":"","nickname":"fixture"}}'\'' > /auths/workbuddy-mock.json
    printf "%s\n" '\''{"accounts":{"mock":{"credits":77,"err_total":4}}}'\'' > /data/state.json
    printf "%s\n" '\''{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"legacy-api"}'\'' > /data/console-keys.json
    chmod 600 /auths/workbuddy-mock.json /data/state.json /data/console-keys.json
    chown -R 10001:10001 /auths /data
  '
legacy_before="$(docker run --rm --network none \
  --mount "type=volume,src=${legacy_project}_auths,dst=/auths,readonly" \
  --mount "type=volume,src=${legacy_project}_data,dst=/data,readonly" \
  alpine:3.20 sh -eu -c 'sha256sum /auths/workbuddy-mock.json /data/state.json /data/console-keys.json')"
compose_for "$legacy_project" "$legacy_port" up -d --wait --no-build
[[ "$(compose_for "$legacy_project" "$legacy_port" port console 7863)" == "127.0.0.1:$legacy_port" ]]
wait_live "http://127.0.0.1:$legacy_port/livez"
[[ "$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer legacy-api' "http://127.0.0.1:$legacy_port/status")" == "200" ]]
curl --noproxy '*' -fsS -H "Origin: http://127.0.0.1:$legacy_port" -H 'Content-Type: application/json' \
  --data '{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}' "http://127.0.0.1:$legacy_port/admin/login" >/dev/null
curl --noproxy '*' -fsS -H 'Authorization: Bearer legacy-api' "http://127.0.0.1:$legacy_port/v1/models" | python3 -c '
import json,sys
models=json.load(sys.stdin)["data"]
assert any(model["id"] == "global:mock-model" for model in models)
'
mock_stream="$(curl --noproxy '*' -fsSN -H 'Authorization: Bearer legacy-api' -H 'Content-Type: application/json' \
  --data '{"model":"global:mock-model","messages":[{"role":"user","content":"acceptance"}],"stream":true}' \
  "http://127.0.0.1:$legacy_port/v1/chat/completions")"
[[ "$mock_stream" == *"mock-runtime-ok"* && "$mock_stream" == *"data: [DONE]"* ]]
curl --noproxy '*' -fsS -H 'Authorization: Bearer legacy-api' "http://127.0.0.1:$legacy_port/status" | python3 -c '
import json,sys
status=json.load(sys.stdin)
assert status["total"] == 1
assert len(status["accounts"]) == 1
account=status["accounts"][0]
assert account["uid"] == "mock" and account["realm"] == "global"
assert account["credits"] == 77
assert account["err_total"] == 4
'
compose_for "$legacy_project" "$legacy_port" exec -T core python3 -c '
import json
old=json.load(open("/app/data/console-keys.json"))
new=json.load(open("/run/wb2a/keys.json"))
assert old == {"admin_key":"a"*32,"api_key":"legacy-api"}
assert new["admin_key"] == old["admin_key"] and new["api_key"] == old["api_key"]
assert len(new["bridge_key"]) >= 32 and new["bridge_key"] not in old.values()
assert json.load(open("/app/data/state.json"))["accounts"]["mock"] == {"credits":77,"err_total":4}
auth=json.load(open("/app/auths/workbuddy-mock.json"))
assert auth["account"]["uid"] == "mock" and auth["auth"]["realm"] == "global"
'
login_json="$(curl --noproxy '*' -fsS -c "$cookie_file" -H "Origin: http://127.0.0.1:$legacy_port" -H 'Content-Type: application/json' \
  --data '{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}' "http://127.0.0.1:$legacy_port/admin/login")"
csrf="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["csrf"])' "$login_json")"
curl --noproxy '*' -fsS -b "$cookie_file" "http://127.0.0.1:$legacy_port/admin/tasks" | python3 -c '
import json,sys
body=json.load(sys.stdin)
checkin=next(task for task in body["items"] if task["id"] == "checkin")
assert checkin["enabled"] is True and body["active_run"] is None
assert checkin["hours"] == [4]
assert all(task["enabled"] is False for task in body["items"] if task["id"] != "checkin")
'
[[ "$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -b "$cookie_file" \
  -H "Origin: http://127.0.0.1:$legacy_port" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
  --data '{"request_id":"disabled-task-acceptance"}' "http://127.0.0.1:$legacy_port/admin/tasks/travel/runs")" == "409" ]]
request_id="acceptance-run-$suffix"
run_json="$(curl --noproxy '*' -fsS -b "$cookie_file" -H "Origin: http://127.0.0.1:$legacy_port" \
  -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' --data "{\"request_id\":\"$request_id\"}" \
  "http://127.0.0.1:$legacy_port/admin/tasks/checkin/runs")"
run_id="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["id"])' "$run_json")"
for _ in {1..100}; do
  run_json="$(curl --noproxy '*' -fsS -b "$cookie_file" "http://127.0.0.1:$legacy_port/admin/task-runs/$run_id")"
  python3 -c 'import json,sys; raise SystemExit(json.loads(sys.argv[1])["finished_at"] is None)' "$run_json" && break
  sleep 0.1
done
python3 -c '
import json,sys
run=json.loads(sys.argv[1])
assert run["status"] == "skipped" and run["request_id"] == sys.argv[2]
assert run["accounts"] == [{"uid":"mock","status":"skipped","detail":"global","before":None,"after":None,"reward":None}]
' "$run_json" "$request_id"
legacy_key_digest="$(compose_for "$legacy_project" "$legacy_port" exec -T core sha256sum /run/wb2a/keys.json | cut -d' ' -f1)"
compose_for "$legacy_project" "$legacy_port" up -d --force-recreate --wait --no-build
[[ "$legacy_key_digest" == "$(compose_for "$legacy_project" "$legacy_port" exec -T core sha256sum /run/wb2a/keys.json | cut -d' ' -f1)" ]]
login_json="$(curl --noproxy '*' -fsS -c "$cookie_file" -H "Origin: http://127.0.0.1:$legacy_port" -H 'Content-Type: application/json' \
  --data '{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}' "http://127.0.0.1:$legacy_port/admin/login")"
curl --noproxy '*' -fsS -b "$cookie_file" "http://127.0.0.1:$legacy_port/admin/task-runs/$run_id" | python3 -c '
import json,sys
run=json.load(sys.stdin)
assert run["id"] == sys.argv[1] and run["status"] == "skipped" and run["finished_at"] is not None
' "$run_id"
legacy_after="$(compose_for "$legacy_project" "$legacy_port" exec -T core sh -eu -c \
  'sha256sum /app/auths/workbuddy-mock.json /app/data/state.json /app/data/console-keys.json')"
[[ "$legacy_before" == "$legacy_after" ]]

acceptance_passed=true
echo "acceptance passed: fresh/rebuild/legacy; isolated mock URL http://127.0.0.1:$legacy_port/"
