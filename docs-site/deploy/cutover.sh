#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

SCRIPT_NAME="$(basename "$0")"
RELEASE_ID="${SCRIPT_NAME%-cutover.sh}"
ROOT="/opt/1panel/www/sites/doc.api.onprs.top"
RELEASE_DIR="$ROOT/releases/$RELEASE_ID"
CURRENT_LINK="$ROOT/index"
CONF_PATH="/opt/1panel/www/conf.d/doc.api.onprs.top.conf"
STATE_PATH="$ROOT/.rollback-$RELEASE_ID"
DOCS_HOST="doc.api.onprs.top"
PUBLIC_DOCS_URL="https://$DOCS_HOST"
ERROR_LOG="$ROOT/log/error.log"
OLD_TARGET=""
SERVICE_PID_BEFORE=""
OPENRESTY_MASTER_PID_BEFORE=""
CONFIG_SHA256_BEFORE=""
ERROR_LOG_LINES_BEFORE=0
CONTAINER=""

if [[ ! "$RELEASE_ID" =~ ^docs-[A-Za-z0-9._-]+$ ]]; then
  echo "Invalid release ID derived from script name: $RELEASE_ID" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this script as root." >&2
  exit 1
fi

exec 9>"/tmp/doc-api-onprs-top-deploy.lock"
flock -n 9 || { echo "Another docs deployment is in progress." >&2; exit 1; }

log() {
  printf '[docs-cutover] %s\n' "$*"
}

find_openresty_container() {
  mapfile -t containers < <(docker ps --format '{{.Names}} {{.Image}}' | awk 'tolower($0) ~ /openresty/ {print $1}')
  if [[ "${#containers[@]}" -ne 1 ]]; then
    echo "Expected exactly one running OpenResty container, found ${#containers[@]}." >&2
    return 1
  fi
  CONTAINER="${containers[0]}"
}

openresty_master_pid() {
  docker inspect -f '{{.State.Pid}}' "$CONTAINER"
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

switch_current() {
  local target="$1"
  local tmp="$ROOT/.index-$RELEASE_ID-$$"
  rm -f "$tmp"
  ln -s "$target" "$tmp"
  mv -Tf "$tmp" "$CURRENT_LINK"
}

write_state() {
  local tmp="$STATE_PATH.tmp.$$"
  {
    printf 'release_id=%s\n' "$RELEASE_ID"
    printf 'old_target=%s\n' "$OLD_TARGET"
    printf 'config_sha256=%s\n' "$CONFIG_SHA256_BEFORE"
  } >"$tmp"
  chmod 0600 "$tmp"
  mv -f "$tmp" "$STATE_PATH"
}

validate_panel_config() {
  [[ -f "$CONF_PATH" ]]
  grep -Eq '^[[:space:]]*server_name[[:space:]]+doc\.api\.onprs\.top;' "$CONF_PATH"
  grep -Fq 'root /www/sites/doc.api.onprs.top/index;' "$CONF_PATH"
  grep -Fq 'try_files $uri $uri.html $uri/ =404;' "$CONF_PATH"
  grep -Fq 'error_page 404 /404.html;' "$CONF_PATH"
  docker exec "$CONTAINER" nginx -t
}

validate_main_service() {
  systemctl is-active --quiet sub2api
  wait_http http://127.0.0.1:8080/health 200
  wait_http https://api.onprs.top/health 200
  [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" == "true" ]]
  ss -ltnp | grep -Eq '0\.0\.0\.0:80[[:space:]].*openresty'
  ss -ltnp | grep -Eq '0\.0\.0\.0:443[[:space:]].*openresty'
}

validate_docs_service() {
  local resolve=(--resolve "$DOCS_HOST:443:127.0.0.1")
  wait_http "$PUBLIC_DOCS_URL/?release=$RELEASE_ID" 200 "${resolve[@]}"
  wait_http "$PUBLIC_DOCS_URL/getting-started/first-request?release=$RELEASE_ID" 200 "${resolve[@]}"
  wait_http "$PUBLIC_DOCS_URL/definitely-not-a-doc-page-$RELEASE_ID" 404 "${resolve[@]}"
  wait_http "$PUBLIC_DOCS_URL/?release=$RELEASE_ID" 200
  wait_http "$PUBLIC_DOCS_URL/getting-started/first-request?release=$RELEASE_ID" 200
  wait_http "$PUBLIC_DOCS_URL/definitely-not-a-doc-page-$RELEASE_ID" 404

  local expected_commit actual_commit asset_file asset_path asset_headers html_headers
  expected_commit="$(cat "$RELEASE_DIR/git-commit.txt")"
  actual_commit="$(curl -fsS --max-time 15 "${resolve[@]}" "$PUBLIC_DOCS_URL/git-commit.txt?release=$RELEASE_ID")"
  [[ "$actual_commit" == "$expected_commit" ]]

  asset_file="$(find "$CURRENT_LINK/assets" -maxdepth 1 -type f -print -quit)"
  [[ -n "$asset_file" ]]
  asset_path="${asset_file#"$CURRENT_LINK"}"
  asset_headers="$(curl -fsSI --max-time 15 "${resolve[@]}" "$PUBLIC_DOCS_URL$asset_path")"
  grep -Eiq '^cache-control:.*max-age=31536000.*immutable' <<<"$asset_headers"

  html_headers="$(curl -fsSI --max-time 15 "${resolve[@]}" "$PUBLIC_DOCS_URL/")"
  grep -Eiq '^cache-control:.*no-cache' <<<"$html_headers"
  grep -Eiq '^strict-transport-security:' <<<"$html_headers"
  grep -Eiq '^x-content-type-options:[[:space:]]*nosniff' <<<"$html_headers"
  grep -Eiq '^x-frame-options:[[:space:]]*DENY' <<<"$html_headers"
  grep -Eiq '^content-security-policy:' <<<"$html_headers"
}

rollback_on_error() {
  local status="$?"
  trap - ERR
  set +e
  log "cutover failed with exit_code=$status; restoring $OLD_TARGET"
  if [[ -n "$OLD_TARGET" && -d "$ROOT/$OLD_TARGET" ]]; then
    switch_current "$OLD_TARGET"
  fi
  validate_main_service
  if [[ -n "$OLD_TARGET" ]]; then
    wait_http "$PUBLIC_DOCS_URL/" 200 --resolve "$DOCS_HOST:443:127.0.0.1"
  fi
  log "automatic rollback finished"
  exit "$status"
}
trap rollback_on_error ERR

log "validating staged release $RELEASE_ID"
[[ -d "$RELEASE_DIR" ]]
[[ -f "$RELEASE_DIR/index.html" ]]
[[ -f "$RELEASE_DIR/404.html" ]]
[[ -f "$RELEASE_DIR/getting-started/index.html" ]]
[[ -f "$RELEASE_DIR/robots.txt" ]]
[[ -f "$RELEASE_DIR/git-commit.txt" ]]
[[ -f "$RELEASE_DIR/archive-sha256.txt" ]]
grep -Fq 'https://cdn.api.onprs.top/' "$RELEASE_DIR/index.html"

find_openresty_container
SERVICE_PID_BEFORE="$(systemctl show sub2api -p MainPID --value)"
OPENRESTY_MASTER_PID_BEFORE="$(openresty_master_pid)"
CONFIG_SHA256_BEFORE="$(sha256sum "$CONF_PATH" | awk '{print $1}')"
[[ -n "$SERVICE_PID_BEFORE" && "$SERVICE_PID_BEFORE" != "0" ]]
[[ -n "$OPENRESTY_MASTER_PID_BEFORE" && "$OPENRESTY_MASTER_PID_BEFORE" != "0" ]]
if [[ -f "$ERROR_LOG" ]]; then
  ERROR_LOG_LINES_BEFORE="$(wc -l <"$ERROR_LOG")"
fi
validate_panel_config
validate_main_service

[[ -L "$CURRENT_LINK" ]] || { echo "$CURRENT_LINK is not a symlink." >&2; exit 1; }
OLD_TARGET="$(readlink "$CURRENT_LINK")"
if [[ ! "$OLD_TARGET" =~ ^releases/[A-Za-z0-9._-]+$ || ! -d "$ROOT/$OLD_TARGET" ]]; then
  echo "$CURRENT_LINK points to an invalid release target: $OLD_TARGET" >&2
  exit 1
fi
write_state

log "switching index symlink to releases/$RELEASE_ID"
switch_current "releases/$RELEASE_ID"
[[ "$(readlink "$CURRENT_LINK")" == "releases/$RELEASE_ID" ]]
validate_docs_service
validate_main_service
validate_panel_config

[[ "$(sha256sum "$CONF_PATH" | awk '{print $1}')" == "$CONFIG_SHA256_BEFORE" ]]
[[ "$(systemctl show sub2api -p MainPID --value)" == "$SERVICE_PID_BEFORE" ]]
[[ "$(openresty_master_pid)" == "$OPENRESTY_MASTER_PID_BEFORE" ]]
if [[ -f "$ERROR_LOG" ]]; then
  if awk -v start="$((ERROR_LOG_LINES_BEFORE + 1))" '
    NR >= start && tolower($0) ~ /\[(emerg|alert|crit)\]/ { found = 1 }
    END { exit(found ? 0 : 1) }
  ' "$ERROR_LOG"; then
    echo "OpenResty wrote a critical docs-site error during cutover." >&2
    false
  fi
fi

trap - ERR
log "cutover successful; OpenResty was not reloaded"
printf 'release_id=%s\n' "$RELEASE_ID"
printf 'previous_target=%s\n' "$OLD_TARGET"
printf 'current_target=%s\n' "$(readlink "$CURRENT_LINK")"
printf 'git_commit=%s\n' "$(cat "$RELEASE_DIR/git-commit.txt")"
printf 'archive_sha256=%s\n' "$(cat "$RELEASE_DIR/archive-sha256.txt")"
printf 'config_sha256=%s\n' "$CONFIG_SHA256_BEFORE"
printf 'openresty_master_pid_before=%s\n' "$OPENRESTY_MASTER_PID_BEFORE"
printf 'openresty_master_pid_after=%s\n' "$(openresty_master_pid)"
printf 'sub2api_pid_before=%s\n' "$SERVICE_PID_BEFORE"
printf 'sub2api_pid_after=%s\n' "$(systemctl show sub2api -p MainPID --value)"
printf 'public_docs_http=200\npublic_docs_url=%s/\n' "$PUBLIC_DOCS_URL"
printf 'rollback_command=bash /tmp/%s-rollback.sh\n' "$RELEASE_ID"
