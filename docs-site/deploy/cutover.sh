#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

SCRIPT_NAME="$(basename "$0")"
RELEASE_ID="${SCRIPT_NAME%-cutover.sh}"
ROOT="/opt/1panel/www/sites/sub2api-docs"
RELEASE_DIR="$ROOT/releases/$RELEASE_ID"
CURRENT_LINK="$ROOT/current"
CONF_DIR="/opt/1panel/www/conf.d"
CONF_PATH="$CONF_DIR/sub2api-docs-port.conf"
CANDIDATE="/tmp/$RELEASE_ID-openresty.conf"
LOGROTATE_PATH="/etc/logrotate.d/sub2api-docs"
LOGROTATE_CANDIDATE="/tmp/$RELEASE_ID-logrotate.conf"
STATE_PATH="$ROOT/.rollback-$RELEASE_ID"
PUBLIC_DOCS_URL="${PUBLIC_DOCS_URL:-http://49.235.188.225:4173}"
REHEARSE_CONFIG_ROLLBACK="${REHEARSE_CONFIG_ROLLBACK:-1}"
CONFIG_ADDED=0
LOGROTATE_ADDED=0
OLD_TARGET=""
SERVICE_PID_BEFORE=""
OPENRESTY_MASTER_PID_BEFORE=""
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

exec 9>"/tmp/sub2api-docs-deploy.lock"
if ! flock -n 9; then
  echo "Another docs deployment is in progress." >&2
  exit 1
fi

log() {
  printf '[docs-cutover] %s\n' "$*"
}

fail() {
  printf '%s\n' "$*" >&2
  return 1
}

find_openresty_container() {
  mapfile -t containers < <(docker ps --format '{{.Names}} {{.Image}}' | awk 'tolower($0) ~ /openresty/ {print $1}')
  if [[ "${#containers[@]}" -ne 1 ]]; then
    echo "Expected exactly one running OpenResty container, found ${#containers[@]}." >&2
    return 1
  fi
  CONTAINER="${containers[0]}"
}

nginx_test() {
  docker exec "$CONTAINER" nginx -t
}

nginx_dump() {
  local output="$1"
  local diagnostics="$output.stderr"
  docker exec "$CONTAINER" nginx -T >"$output" 2>"$diagnostics"
  chmod 0600 "$output" "$diagnostics"
  grep -Fq 'syntax is ok' "$diagnostics"
  grep -Fq 'test is successful' "$diagnostics"
  if grep -Eiq '\[(emerg|alert|crit)\]' "$diagnostics"; then
    return 1
  fi
}

nginx_reload() {
  docker exec "$CONTAINER" nginx -s reload
}

openresty_master_pid() {
  docker inspect -f '{{.State.Pid}}' "$CONTAINER"
}

conf_manifest() {
  local output="$1"
  find "$CONF_DIR" -maxdepth 1 -type f ! -name 'sub2api-docs-port.conf' -print0 \
    | sort -z \
    | xargs -0r sha256sum >"$output"
  chmod 0600 "$output"
}

write_state() {
  local tmp="$STATE_PATH.tmp.$$"
  {
    printf 'release_id=%s\n' "$RELEASE_ID"
    printf 'old_target=%s\n' "$OLD_TARGET"
    printf 'config_added=%s\n' "$CONFIG_ADDED"
    printf 'logrotate_added=%s\n' "$LOGROTATE_ADDED"
  } >"$tmp"
  chmod 0600 "$tmp"
  mv -f "$tmp" "$STATE_PATH"
}

switch_current() {
  local target="$1"
  local tmp="$ROOT/.current-$RELEASE_ID-$$"
  rm -f "$tmp"
  ln -s "$target" "$tmp"
  mv -Tf "$tmp" "$CURRENT_LINK"
}

restore_previous_current() {
  if [[ -n "$OLD_TARGET" && -d "$ROOT/$OLD_TARGET" ]]; then
    switch_current "$OLD_TARGET"
  else
    rm -f "$CURRENT_LINK"
  fi
}

wait_http() {
  local url="$1"
  local expected="$2"
  local code=""
  for _ in $(seq 1 30); do
    code="$(curl -sS -o /dev/null --connect-timeout 3 --max-time 8 -w '%{http_code}' "$url" || true)"
    if [[ "$code" == "$expected" ]]; then
      printf '%s' "$code"
      return 0
    fi
    sleep 1
  done
  echo "Expected HTTP $expected from $url, got ${code:-none}." >&2
  return 1
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
  echo "Port 4173 still served HTTP $code after configuration rollback." >&2
  return 1
}

validate_main_service() {
  systemctl is-active --quiet sub2api
  [[ "$(wait_http http://127.0.0.1:8080/health 200)" == "200" ]]
  [[ "$(wait_http https://api.onprs.top/health 200)" == "200" ]]
  [[ "$(wait_http https://api.onprs.top/ 200)" == "200" ]]
  ss -ltnp | grep -Eq '0\.0\.0\.0:80[[:space:]].*openresty'
  ss -ltnp | grep -Eq '0\.0\.0\.0:443[[:space:]].*openresty'
  [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" == "true" ]]
}

validate_docs_service() {
  [[ "$(wait_http http://127.0.0.1:4173/ 200)" == "200" ]]
  [[ "$(wait_http http://127.0.0.1:4173/getting-started/ 200)" == "200" ]]
  [[ "$(wait_http http://127.0.0.1:4173/getting-started/first-request 200)" == "200" ]]
  [[ "$(wait_http http://127.0.0.1:4173/404.html 200)" == "200" ]]
  [[ "$(wait_http http://127.0.0.1:4173/robots.txt 200)" == "200" ]]
  [[ "$(wait_http http://127.0.0.1:4173/definitely-not-a-doc-page 404)" == "404" ]]
  [[ "$(wait_http "$PUBLIC_DOCS_URL/" 200)" == "200" ]]
  [[ "$(wait_http "$PUBLIC_DOCS_URL/getting-started/first-request" 200)" == "200" ]]
  ss -ltnp | grep -Eq '0\.0\.0\.0:4173[[:space:]].*openresty'

  local asset_file asset_path asset_headers html_headers
  asset_file="$(find "$CURRENT_LINK/assets" -maxdepth 1 -type f -print -quit)"
  [[ -n "$asset_file" ]]
  asset_path="${asset_file#"$CURRENT_LINK"}"
  asset_headers="$(curl -sSI --max-time 10 "http://127.0.0.1:4173$asset_path")"
  grep -Eiq '^cache-control:.*max-age=31536000.*immutable' <<<"$asset_headers"
  html_headers="$(curl -sSI --max-time 10 http://127.0.0.1:4173/)"
  grep -Eiq '^cache-control:.*no-cache' <<<"$html_headers"
  grep -Eiq '^x-content-type-options:[[:space:]]*nosniff' <<<"$html_headers"
  grep -Eiq '^x-frame-options:[[:space:]]*DENY' <<<"$html_headers"
  grep -Eiq '^content-security-policy:' <<<"$html_headers"
}

validate_candidate() {
  [[ -f "$CANDIDATE" ]]
  [[ "$(grep -Ec '^[[:space:]]*server[[:space:]]*\{' "$CANDIDATE")" -eq 1 ]]
  [[ "$(grep -Ec '^[[:space:]]*listen[[:space:]]+0\.0\.0\.0:4173;' "$CANDIDATE")" -eq 1 ]]
  grep -Fq 'root /www/sites/sub2api-docs/current;' "$CANDIDATE"
  grep -Fq 'try_files $uri $uri.html $uri/ =404;' "$CANDIDATE"
  grep -Fq 'error_page 404 /404.html;' "$CANDIDATE"

  if grep -Eiq '^[[:space:]]*listen[[:space:]]+([^;]*:)?(80|443)([[:space:];]|$)|default_server|proxy_pass|api\.onprs\.top|^[[:space:]]*upstream[[:space:]]|^[[:space:]]*include[[:space:]]' "$CANDIDATE"; then
    echo "Candidate contains a forbidden shared-service directive." >&2
    return 1
  fi
}

install_candidate() {
  local tmp="$CONF_DIR/.sub2api-docs-port.conf.$RELEASE_ID.$$"
  install -m 0644 "$CANDIDATE" "$tmp"
  mv -f "$tmp" "$CONF_PATH"
}

rollback_on_error() {
  local status="$?"
  trap - ERR
  set +e
  log "cutover failed with exit_code=$status; restoring previous state"
  restore_previous_current
  if [[ "$CONFIG_ADDED" -eq 1 && -f "$CONF_PATH" ]]; then
    rm -f "$CONF_PATH"
    if nginx_test; then
      nginx_reload
      sleep 2
    fi
  fi
  if [[ "$LOGROTATE_ADDED" -eq 1 ]]; then
    rm -f "$LOGROTATE_PATH"
  fi
  validate_main_service
  write_state
  log "automatic rollback finished; run /tmp/$RELEASE_ID-rollback.sh for an explicit recheck"
  exit "$status"
}
trap rollback_on_error ERR

log "validating staged release $RELEASE_ID"
[[ -d "$RELEASE_DIR" ]]
[[ -f "$RELEASE_DIR/index.html" ]]
[[ -f "$RELEASE_DIR/404.html" ]]
[[ -f "$RELEASE_DIR/getting-started/index.html" ]]
[[ -f "$RELEASE_DIR/robots.txt" ]]
find_openresty_container
SERVICE_PID_BEFORE="$(systemctl show sub2api -p MainPID --value)"
OPENRESTY_MASTER_PID_BEFORE="$(openresty_master_pid)"
[[ -n "$SERVICE_PID_BEFORE" && "$SERVICE_PID_BEFORE" != "0" ]]
[[ -n "$OPENRESTY_MASTER_PID_BEFORE" && "$OPENRESTY_MASTER_PID_BEFORE" != "0" ]]
if [[ -f "$ROOT/log/error.log" ]]; then
  ERROR_LOG_LINES_BEFORE="$(wc -l <"$ROOT/log/error.log")"
fi
validate_main_service

if [[ -L "$CURRENT_LINK" ]]; then
  candidate_old_target="$(readlink "$CURRENT_LINK")"
  if [[ ! "$candidate_old_target" =~ ^releases/docs-[A-Za-z0-9._-]+$ || ! -d "$ROOT/$candidate_old_target" ]]; then
    echo "$CURRENT_LINK points to an invalid release target." >&2
    exit 1
  fi
  OLD_TARGET="$candidate_old_target"
elif [[ -e "$CURRENT_LINK" ]]; then
  echo "$CURRENT_LINK exists but is not a symlink." >&2
  exit 1
fi

if [[ -e "$CONF_PATH" ]]; then
  cmp -s "$CONF_PATH" "$CANDIDATE" || {
    echo "$CONF_PATH already exists with different content; refusing to overwrite." >&2
    exit 1
  }
else
  CONFIG_ADDED=1
fi
if [[ -e "$LOGROTATE_PATH" ]]; then
  cmp -s "$LOGROTATE_PATH" "$LOGROTATE_CANDIDATE" || {
    echo "$LOGROTATE_PATH already exists with different content; refusing to overwrite." >&2
    exit 1
  }
else
  LOGROTATE_ADDED=1
fi
write_state

if [[ "$CONFIG_ADDED" -eq 1 ]] && ss -ltn | grep -Eq '(^|[[:space:]])[^[:space:]]*:4173[[:space:]]'; then
  echo "Port 4173 became occupied before the initial docs configuration was installed." >&2
  exit 1
fi

log "switching current symlink to releases/$RELEASE_ID"
switch_current "releases/$RELEASE_ID"
[[ "$(readlink "$CURRENT_LINK")" == "releases/$RELEASE_ID" ]]

if [[ "$CONFIG_ADDED" -eq 1 ]]; then
  log "capturing OpenResty baseline"
  validate_candidate
  nginx_test
  conf_manifest "/tmp/$RELEASE_ID-conf-before.sha256"
  nginx_dump "/tmp/$RELEASE_ID-nginx-before.txt"
  sleep 1
  conf_manifest "/tmp/$RELEASE_ID-conf-preinstall.sha256"
  if ! cmp -s "/tmp/$RELEASE_ID-conf-before.sha256" "/tmp/$RELEASE_ID-conf-preinstall.sha256"; then
    fail "OpenResty conf.d changed concurrently before candidate installation."
  fi

  install_candidate
  nginx_test
  conf_manifest "/tmp/$RELEASE_ID-conf-after.sha256"
  if ! cmp -s "/tmp/$RELEASE_ID-conf-before.sha256" "/tmp/$RELEASE_ID-conf-after.sha256"; then
    fail "A non-docs OpenResty configuration changed during cutover."
  fi
  nginx_dump "/tmp/$RELEASE_ID-nginx-after.txt"

  MARKER='# configuration file /usr/local/openresty/nginx/conf/conf.d/sub2api-docs-port.conf:'
  awk -v marker="$MARKER" '
    $0 == marker { skip = 1; next }
    skip && /^# configuration file / { skip = 0 }
    !skip { print }
  ' "/tmp/$RELEASE_ID-nginx-after.txt" >"/tmp/$RELEASE_ID-nginx-after-without-docs.txt"
  if ! cmp -s "/tmp/$RELEASE_ID-nginx-before.txt" "/tmp/$RELEASE_ID-nginx-after-without-docs.txt"; then
    diff -u "/tmp/$RELEASE_ID-nginx-before.txt" "/tmp/$RELEASE_ID-nginx-after-without-docs.txt" >"/tmp/$RELEASE_ID-nginx-unexpected.diff" || true
    fail "nginx -T changed outside the candidate server block; refusing to reload."
  fi
  diff -u "/tmp/$RELEASE_ID-nginx-before.txt" "/tmp/$RELEASE_ID-nginx-after.txt" >"/tmp/$RELEASE_ID-nginx-docs.diff" || true

  log "performing first graceful reload"
  nginx_reload
  sleep 2
  validate_main_service
  validate_docs_service

  if [[ "$REHEARSE_CONFIG_ROLLBACK" == "1" ]]; then
    log "rehearsing configuration rollback"
    rm -f "$CONF_PATH"
    nginx_test
    nginx_reload
    sleep 2
    validate_main_service
    wait_docs_port_closed

    log "restoring candidate after rollback rehearsal"
    install_candidate
    nginx_test
    nginx_reload
    sleep 2
  fi
else
  log "existing docs configuration matches candidate; content release needs no reload"
fi

validate_main_service
validate_docs_service
[[ "$(systemctl show sub2api -p MainPID --value)" == "$SERVICE_PID_BEFORE" ]]
[[ "$(openresty_master_pid)" == "$OPENRESTY_MASTER_PID_BEFORE" ]]
if [[ -f "$ROOT/log/error.log" ]]; then
  if awk -v start="$((ERROR_LOG_LINES_BEFORE + 1))" '
    NR >= start && tolower($0) ~ /\[(emerg|alert|crit)\]/ { found = 1 }
    END { exit(found ? 0 : 1) }
  ' "$ROOT/log/error.log"; then
    fail "OpenResty wrote a critical docs-site error during cutover."
  fi
fi

if [[ "$LOGROTATE_ADDED" -eq 1 ]]; then
  install -m 0644 "$LOGROTATE_CANDIDATE" "$LOGROTATE_PATH"
fi
write_state

trap - ERR
log "cutover successful"
printf 'release_id=%s\n' "$RELEASE_ID"
printf 'current_target=%s\n' "$(readlink "$CURRENT_LINK")"
printf 'archive_sha256=%s\n' "$(cat "$RELEASE_DIR/archive-sha256.txt")"
printf 'openresty_container=%s\n' "$CONTAINER"
printf 'openresty_master_pid_before=%s\n' "$OPENRESTY_MASTER_PID_BEFORE"
printf 'openresty_master_pid_after=%s\n' "$(openresty_master_pid)"
printf 'config_sha256=%s\n' "$(sha256sum "$CONF_PATH" | awk '{print $1}')"
printf 'sub2api_pid_before=%s\n' "$SERVICE_PID_BEFORE"
printf 'sub2api_pid_after=%s\n' "$(systemctl show sub2api -p MainPID --value)"
printf 'local_docs_http=200\n'
printf 'public_docs_http=200\n'
printf 'public_docs_url=%s/\n' "$PUBLIC_DOCS_URL"
printf 'rollback_command=bash /tmp/%s-rollback.sh\n' "$RELEASE_ID"

tail -n 20 "$ROOT/log/error.log" 2>/dev/null || true
