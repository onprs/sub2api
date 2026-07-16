#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

SCRIPT_NAME="$(basename "$0")"
RELEASE_ID="${SCRIPT_NAME%-rollback.sh}"
ROOT="/opt/1panel/www/sites/sub2api-docs"
CURRENT_LINK="$ROOT/current"
CONF_PATH="/opt/1panel/www/conf.d/sub2api-docs-port.conf"
LOGROTATE_PATH="/etc/logrotate.d/sub2api-docs"
STATE_PATH="$ROOT/.rollback-$RELEASE_ID"
PUBLIC_DOCS_URL="${PUBLIC_DOCS_URL:-http://49.235.188.225:4173}"
ORIGINAL_TARGET=""
OLD_TARGET=""
CONFIG_ADDED=0
LOGROTATE_ADDED=0
CONTAINER=""
CONFIG_BACKUP="/tmp/$RELEASE_ID-config-before-rollback.conf"

if [[ ! "$RELEASE_ID" =~ ^docs-[A-Za-z0-9._-]+$ ]]; then
  echo "Invalid release ID derived from script name: $RELEASE_ID" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this script as root." >&2
  exit 1
fi
[[ -f "$STATE_PATH" ]]

exec 9>"/tmp/sub2api-docs-deploy.lock"
flock -n 9 || { echo "Another docs deployment is in progress." >&2; exit 1; }

state_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "$STATE_PATH" | tail -n 1
}

OLD_TARGET="$(state_value old_target)"
CONFIG_ADDED="$(state_value config_added)"
LOGROTATE_ADDED="$(state_value logrotate_added)"
ORIGINAL_TARGET="$(readlink "$CURRENT_LINK" 2>/dev/null || true)"
mapfile -t containers < <(docker ps --format '{{.Names}} {{.Image}}' | awk 'tolower($0) ~ /openresty/ {print $1}')
[[ "${#containers[@]}" -eq 1 ]]
CONTAINER="${containers[0]}"

nginx_test() { docker exec "$CONTAINER" nginx -t; }
nginx_reload() { docker exec "$CONTAINER" nginx -s reload; }

switch_current() {
  local target="$1"
  local tmp="$ROOT/.rollback-current-$RELEASE_ID-$$"
  rm -f "$tmp"
  ln -s "$target" "$tmp"
  mv -Tf "$tmp" "$CURRENT_LINK"
}

wait_docs_port_closed() {
  local code=""
  for _ in $(seq 1 20); do
    code="$(curl -sS -o /dev/null --connect-timeout 1 --max-time 2 -w '%{http_code}' http://127.0.0.1:4173/ || true)"
    if [[ -z "$code" || "$code" == "000" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "Port 4173 still served HTTP $code after rollback." >&2
  return 1
}

validate_main() {
  systemctl is-active --quiet sub2api
  [[ "$(curl -sS -o /dev/null --max-time 10 -w '%{http_code}' http://127.0.0.1:8080/health)" == "200" ]]
  [[ "$(curl -sS -o /dev/null --max-time 15 -w '%{http_code}' https://api.onprs.top/health)" == "200" ]]
  [[ "$(curl -sS -o /dev/null --max-time 15 -w '%{http_code}' https://api.onprs.top/)" == "200" ]]
  ss -ltnp | grep -Eq '0\.0\.0\.0:80[[:space:]].*openresty'
  ss -ltnp | grep -Eq '0\.0\.0\.0:443[[:space:]].*openresty'
}

restore_rollback_origin() {
  local status="$?"
  trap - ERR
  set +e
  echo "Rollback failed with exit_code=$status; restoring the release being rolled back."
  if [[ -n "$ORIGINAL_TARGET" && -d "$ROOT/$ORIGINAL_TARGET" ]]; then
    switch_current "$ORIGINAL_TARGET"
  fi
  if [[ -f "$CONFIG_BACKUP" ]]; then
    install -m 0644 "$CONFIG_BACKUP" "$CONF_PATH"
  fi
  if nginx_test; then
    nginx_reload
    sleep 2
  fi
  validate_main
  exit "$status"
}
trap restore_rollback_origin ERR

rm -f "$CONFIG_BACKUP"
if [[ -f "$CONF_PATH" ]]; then
  cp -p "$CONF_PATH" "$CONFIG_BACKUP"
fi

if [[ -n "$OLD_TARGET" ]]; then
  [[ "$OLD_TARGET" =~ ^releases/docs-[A-Za-z0-9._-]+$ ]]
  [[ -d "$ROOT/$OLD_TARGET" ]]
  switch_current "$OLD_TARGET"
else
  rm -f "$CURRENT_LINK"
fi

if [[ "$CONFIG_ADDED" == "1" ]]; then
  rm -f "$CONF_PATH"
  nginx_test
  nginx_reload
  sleep 2
fi
if [[ "$LOGROTATE_ADDED" == "1" ]]; then
  rm -f "$LOGROTATE_PATH"
fi

validate_main
if [[ -n "$OLD_TARGET" ]]; then
  [[ "$(curl -sS -o /dev/null --max-time 10 -w '%{http_code}' http://127.0.0.1:4173/)" == "200" ]]
  [[ "$(curl -sS -o /dev/null --max-time 15 -w '%{http_code}' "$PUBLIC_DOCS_URL/")" == "200" ]]
else
  wait_docs_port_closed
fi

trap - ERR
printf 'rollback_ok=true\n'
printf 'release_id=%s\n' "$RELEASE_ID"
printf 'restored_target=%s\n' "${OLD_TARGET:-none}"
printf 'docs_config_present=%s\n' "$([[ -f "$CONF_PATH" ]] && echo true || echo false)"
printf 'sub2api_health=200\n'
