#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_NAME="$(basename "$0")"
RELEASE_ID="${SCRIPT_NAME%-cleanup.sh}"
ROOT="/opt/1panel/www/sites/doc.api.onprs.top"
RELEASE_RETENTION="${DOCS_RELEASE_RETENTION:-3}"
DOCS_URL="https://doc-api.onprs.online"

if [[ ! "$RELEASE_ID" =~ ^docs-[A-Za-z0-9._-]+$ ]]; then
  echo "Invalid release ID derived from script name: $RELEASE_ID" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this script as root." >&2
  exit 1
fi

CURRENT_TARGET="$(readlink "$ROOT/index" 2>/dev/null || true)"
[[ "$CURRENT_TARGET" == "releases/$RELEASE_ID" ]] || {
  echo "Current docs release is $CURRENT_TARGET, not releases/$RELEASE_ID; refusing cleanup." >&2
  exit 1
}
[[ "$(curl -sS -o /dev/null --max-time 15 -w '%{http_code}' "$DOCS_URL/")" == "200" ]]
[[ "$(curl -sS -o /dev/null --max-time 15 -w '%{http_code}' https://cdn-api.onprs.online/health)" == "200" ]]
[[ "$RELEASE_RETENTION" =~ ^[0-9]+$ && "$RELEASE_RETENTION" -ge 2 ]]

PREVIOUS_RELEASE=""
STATE_PATH="$ROOT/.rollback-$RELEASE_ID"
if [[ -f "$STATE_PATH" ]]; then
  PREVIOUS_TARGET="$(sed -n 's/^old_target=//p' "$STATE_PATH" | tail -n 1)"
  if [[ "$PREVIOUS_TARGET" =~ ^releases/docs-[A-Za-z0-9._-]+$ ]]; then
    PREVIOUS_RELEASE="${PREVIOUS_TARGET#releases/}"
  fi
fi

DELETED_RELEASES=0
mapfile -t RELEASES < <(
  find "$ROOT/releases" -mindepth 1 -maxdepth 1 -type d -name 'docs-*' -printf '%T@ %f\n' \
    | sort -nr \
    | awk '{print $2}'
)
for index in "${!RELEASES[@]}"; do
  release="${RELEASES[$index]}"
  [[ "$release" =~ ^docs-[A-Za-z0-9._-]+$ ]] || continue
  if [[ "$index" -lt "$RELEASE_RETENTION" || "$release" == "$RELEASE_ID" || "$release" == "$PREVIOUS_RELEASE" ]]; then
    continue
  fi
  rm -rf -- "$ROOT/releases/$release"
  rm -f -- "$ROOT/.rollback-$release"
  DELETED_RELEASES=$((DELETED_RELEASES + 1))
done

rm -f \
  "/tmp/$RELEASE_ID-upload.tar.gz" \
  "/tmp/$RELEASE_ID-cutover.log" \
  "/tmp/$RELEASE_ID-rollback.log"
rm -f "/tmp/$RELEASE_ID-cutover.sh" "/tmp/$RELEASE_ID-rollback.sh"
rm -f -- "$0"

printf 'cleanup_ok=true\n'
printf 'current_target=%s\n' "$CURRENT_TARGET"
printf 'release_retention=%s\n' "$RELEASE_RETENTION"
printf 'deleted_old_releases=%s\n' "$DELETED_RELEASES"
printf 'protected_previous_release=%s\n' "${PREVIOUS_RELEASE:-none}"
