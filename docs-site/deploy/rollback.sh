#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

SCRIPT_NAME="$(basename "$0")"
RELEASE_ID="${SCRIPT_NAME%-rollback.sh}"
ROOT="/opt/1panel/www/sites/doc.api.onprs.top"
CURRENT_LINK="$ROOT/index"
CONF_PATH="/opt/1panel/www/conf.d/doc.api.onprs.top.conf"
STATE_PATH="$ROOT/.rollback-$RELEASE_ID"
DOCS_HOST="doc.api.onprs.top"
PUBLIC_DOCS_URL="https://$DOCS_HOST"
ORIGINAL_TARGET=""
OLD_TARGET=""
EXPECTED_CONFIG_SHA256=""
SERVICE_PID_BEFORE=""
OPENRESTY_MASTER_PID_BEFORE=""
CONTAINER=""

if [[ ! "$RELEASE_ID" =~ ^docs-[A-Za-z0-9._-]+$ ]]; then
  echo "Invalid release ID derived from script name: $RELEASE_ID" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this script as root." >&2
  exit 1
fi
[[ -f "$STATE_PATH" ]]

exec 9>"/tmp/doc-api-onprs-top-deploy.lock"
flock -n 9 || { echo "Another docs deployment is in progress." >&2; exit 1; }

state_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "$STATE_PATH" | tail -n 1
}

switch_current() {
  local target="$1"
  local tmp="$ROOT/.rollback-index-$RELEASE_ID-$$"
  rm -f "$tmp"
  ln -s "$target" "$tmp"
  mv -Tf "$tmp" "$CURRENT_LINK"
}

wait_http() {
  local url="$1"
  local expected="$2"
  shift 2
  local code=""
  for _ in $(seq 1 30); do
    code="$(curl -sS -o /dev/null --connect-timeout 3 --max-time 10 -w '%{http_code}' "$@" "$url" || true)"
    if [[ "$code" == "$expected" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "Expected HTTP $expected from $url, got ${code:-none}." >&2
  return 1
}

validate_services() {
  systemctl is-active --quiet sub2api
  wait_http http://127.0.0.1:8080/health 200
  wait_http https://api.onprs.top/health 200
  wait_http "$PUBLIC_DOCS_URL/?rollback=$RELEASE_ID" 200 --resolve "$DOCS_HOST:443:127.0.0.1"
  wait_http "$PUBLIC_DOCS_URL/definitely-not-a-doc-page-$RELEASE_ID" 404 --resolve "$DOCS_HOST:443:127.0.0.1"
  wait_http "$PUBLIC_DOCS_URL/?rollback=$RELEASE_ID" 200
  [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" == "true" ]]
}

restore_rollback_origin() {
  local status="$?"
  trap - ERR
  set +e
  echo "Rollback failed with exit_code=$status; restoring $ORIGINAL_TARGET."
  if [[ -n "$ORIGINAL_TARGET" && -d "$ROOT/$ORIGINAL_TARGET" ]]; then
    switch_current "$ORIGINAL_TARGET"
    validate_services
  fi
  exit "$status"
}

OLD_TARGET="$(state_value old_target)"
EXPECTED_CONFIG_SHA256="$(state_value config_sha256)"
ORIGINAL_TARGET="$(readlink "$CURRENT_LINK" 2>/dev/null || true)"
[[ "$ORIGINAL_TARGET" == "releases/$RELEASE_ID" ]] || {
  echo "Current docs release is $ORIGINAL_TARGET, not releases/$RELEASE_ID; refusing rollback." >&2
  exit 1
}
[[ "$OLD_TARGET" =~ ^releases/[A-Za-z0-9._-]+$ ]]
[[ -d "$ROOT/$OLD_TARGET" ]]
[[ -f "$CONF_PATH" ]]
[[ -n "$EXPECTED_CONFIG_SHA256" ]]
[[ "$(sha256sum "$CONF_PATH" | awk '{print $1}')" == "$EXPECTED_CONFIG_SHA256" ]]

mapfile -t containers < <(docker ps --format '{{.Names}} {{.Image}}' | awk 'tolower($0) ~ /openresty/ {print $1}')
[[ "${#containers[@]}" -eq 1 ]]
CONTAINER="${containers[0]}"
SERVICE_PID_BEFORE="$(systemctl show sub2api -p MainPID --value)"
OPENRESTY_MASTER_PID_BEFORE="$(docker inspect -f '{{.State.Pid}}' "$CONTAINER")"
docker exec "$CONTAINER" nginx -t
trap restore_rollback_origin ERR

switch_current "$OLD_TARGET"
[[ "$(readlink "$CURRENT_LINK")" == "$OLD_TARGET" ]]
validate_services

[[ "$(sha256sum "$CONF_PATH" | awk '{print $1}')" == "$EXPECTED_CONFIG_SHA256" ]]
[[ "$(systemctl show sub2api -p MainPID --value)" == "$SERVICE_PID_BEFORE" ]]
[[ "$(docker inspect -f '{{.State.Pid}}' "$CONTAINER")" == "$OPENRESTY_MASTER_PID_BEFORE" ]]

trap - ERR
printf 'rollback_ok=true\n'
printf 'release_id=%s\n' "$RELEASE_ID"
printf 'restored_target=%s\n' "$OLD_TARGET"
printf 'config_sha256=%s\n' "$EXPECTED_CONFIG_SHA256"
printf 'openresty_reloaded=false\n'
printf 'sub2api_health=200\n'
