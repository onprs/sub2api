#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

REMOTE_HOST="${REMOTE_HOST:-sub2api_tokyo}"
GIT_SHA="$(git rev-parse --short HEAD)"
RELEASE_ID="${RELEASE_ID:-docs-${GIT_SHA}-$(date +%Y%m%d%H%M%S)}"
ARTIFACT_DIR="$REPO_ROOT/artifacts/$RELEASE_ID"
ARCHIVE="$ARTIFACT_DIR/$RELEASE_ID.tar.gz"
UPLOAD_BUNDLE="$ARTIFACT_DIR/$RELEASE_ID-upload.tar.gz"
REMOTE_ROOT="/opt/1panel/www/sites/doc.api.onprs.top"

if [[ ! "$RELEASE_ID" =~ ^docs-[A-Za-z0-9._-]+$ ]]; then
  echo "Invalid release ID: $RELEASE_ID" >&2
  exit 1
fi

if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo "Working tree must be clean before staging a production docs release." >&2
  exit 1
fi

corepack pnpm@9.15.9 --dir docs-site install --frozen-lockfile
corepack pnpm@9.15.9 --dir docs-site run typecheck
corepack pnpm@9.15.9 --dir docs-site run build
test ! -e docs-site/.vitepress/dist/deployment.html
test ! -e docs-site/.vitepress/dist/readme.html
corepack pnpm@9.15.9 --dir docs-site run check:external
corepack pnpm@9.15.9 --dir docs-site run test:smoke

mkdir -p "$ARTIFACT_DIR"
tar -C docs-site/.vitepress/dist -czf "$ARCHIVE" .
SHA256="$(sha256sum "$ARCHIVE" | awk '{print $1}')"
printf '%s  %s\n' "$SHA256" "$(basename "$ARCHIVE")" >"$ARCHIVE.sha256"

cp docs-site/deploy/cutover.sh "$ARTIFACT_DIR/$RELEASE_ID-cutover.sh"
cp docs-site/deploy/rollback.sh "$ARTIFACT_DIR/$RELEASE_ID-rollback.sh"
cp docs-site/deploy/cleanup.sh "$ARTIFACT_DIR/$RELEASE_ID-cleanup.sh"

tar -C "$ARTIFACT_DIR" -czf "$UPLOAD_BUNDLE" \
  "$RELEASE_ID.tar.gz" \
  "$RELEASE_ID.tar.gz.sha256" \
  "$RELEASE_ID-cutover.sh" \
  "$RELEASE_ID-rollback.sh" \
  "$RELEASE_ID-cleanup.sh"
UPLOAD_SHA256="$(sha256sum "$UPLOAD_BUNDLE" | awk '{print $1}')"

stage_remote() {
  ssh -o ConnectTimeout=15 "$REMOTE_HOST" "set -Eeuo pipefail
    umask 077
    root='$REMOTE_ROOT'
    release_id='$RELEASE_ID'
    release_dir=\"\$root/releases/\$release_id\"
    staging_dir=\"\$root/releases/.staging-\$release_id\"
    upload_bundle=\"/tmp/\$release_id-upload.tar.gz\"
    upload_dir=\"/tmp/.docs-upload-\$release_id\"
    release_preexisting=0
    success=0

    cleanup() {
      rm -f \"\$upload_bundle\"
      rm -rf \"\$upload_dir\" \"\$staging_dir\"
      if [[ \"\$success\" -ne 1 ]]; then
        rm -f \
          \"/tmp/\$release_id-cutover.sh\" \
          \"/tmp/\$release_id-rollback.sh\" \
          \"/tmp/\$release_id-cleanup.sh\"
        if [[ \"\$release_preexisting\" -eq 0 ]]; then
          rm -rf \"\$release_dir\"
        fi
      fi
    }
    trap cleanup EXIT

    test -d \"\$root\"
    test -d \"\$root/releases\"
    test -L \"\$root/index\"
    test -f /opt/1panel/www/conf.d/doc.api.onprs.top.conf

    if [[ -e \"\$release_dir\" ]]; then
      test -f \"\$release_dir/archive-sha256.txt\"
      test \"\$(cat \"\$release_dir/archive-sha256.txt\")\" = '$SHA256'
      release_preexisting=1
    fi
    test ! -e \"\$staging_dir\"

    cat >\"\$upload_bundle\"
    printf '%s  %s\n' '$UPLOAD_SHA256' \"\$upload_bundle\" | sha256sum -c -
    mkdir \"\$upload_dir\"
    tar --no-same-owner -xzf \"\$upload_bundle\" -C \"\$upload_dir\"

    cd \"\$upload_dir\"
    test -f \"\$release_id.tar.gz\"
    test -f \"\$release_id.tar.gz.sha256\"
    test -f \"\$release_id-cutover.sh\"
    test -f \"\$release_id-rollback.sh\"
    test -f \"\$release_id-cleanup.sh\"
    printf '%s  %s\n' '$SHA256' \"\$release_id.tar.gz\" | sha256sum -c -

    install -m 0755 \"\$release_id-cutover.sh\" \"/tmp/\$release_id-cutover.sh\"
    install -m 0755 \"\$release_id-rollback.sh\" \"/tmp/\$release_id-rollback.sh\"
    install -m 0755 \"\$release_id-cleanup.sh\" \"/tmp/\$release_id-cleanup.sh\"

    if [[ \"\$release_preexisting\" -eq 0 ]]; then
      mkdir \"\$staging_dir\"
      tar --no-same-owner -xzf \"\$release_id.tar.gz\" -C \"\$staging_dir\"
      test -f \"\$staging_dir/index.html\"
      test -f \"\$staging_dir/404.html\"
      test -f \"\$staging_dir/getting-started/index.html\"
      test -d \"\$staging_dir/assets\"
      grep -Fq 'https://cdn-api.onprs.online/' \"\$staging_dir/index.html\"
      printf '%s\n' '$GIT_SHA' >\"\$staging_dir/git-commit.txt\"
      printf '%s\n' '$SHA256' >\"\$staging_dir/archive-sha256.txt\"
      chown -R root:root \"\$staging_dir\"
      find \"\$staging_dir\" -type d -exec chmod 0755 {} +
      find \"\$staging_dir\" -type f -exec chmod 0644 {} +
      mv \"\$staging_dir\" \"\$release_dir\"
    fi

    success=1
    ls -ld \"\$release_dir\"
    ls -l \"\$release_dir/index.html\" \"\$release_dir/404.html\"
    printf 'remote_stage_ok=true\\nrelease_preexisting=%s\\n' \"\$release_preexisting\"
  " <"$UPLOAD_BUNDLE"
}

STAGED=0
for attempt in 1 2 3 4 5; do
  if stage_remote; then
    STAGED=1
    break
  fi
  if [[ "$attempt" -lt 5 ]]; then
    echo "Remote staging attempt $attempt failed; retrying in 20 seconds." >&2
    sleep 20
  fi
done
if [[ "$STAGED" -ne 1 ]]; then
  echo "Unable to stage $RELEASE_ID on $REMOTE_HOST." >&2
  exit 1
fi

printf 'release_id=%s\n' "$RELEASE_ID"
printf 'git_commit=%s\n' "$GIT_SHA"
printf 'archive_sha256=%s\n' "$SHA256"
printf 'upload_sha256=%s\n' "$UPLOAD_SHA256"
printf 'cutover_command=bash /tmp/%s-cutover.sh 2>&1 | tee /tmp/%s-cutover.log\n' "$RELEASE_ID" "$RELEASE_ID"
printf 'rollback_command=bash /tmp/%s-rollback.sh 2>&1 | tee /tmp/%s-rollback.log\n' "$RELEASE_ID" "$RELEASE_ID"
printf 'cleanup_command=bash /tmp/%s-cleanup.sh 2>&1 | tee /tmp/%s-cleanup.log\n' "$RELEASE_ID" "$RELEASE_ID"
