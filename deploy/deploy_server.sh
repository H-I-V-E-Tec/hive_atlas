#!/usr/bin/env bash
# Deploy a verified native Atlas bundle and an immutable library snapshot.
set -Eeuo pipefail
umask 077

INSTALL_ROOT="${HIVE_ATLAS_INSTALL_ROOT:-/opt/hive-atlas}"
PRIVATE_ROOT="${HIVE_ATLAS_PRIVATE_ROOT:-/srv/hive-private}"
HISTORY_DIR="${HIVE_ATLAS_HISTORY_DIR:-/var/lib/hive-atlas-deploy}"
SYSTEMD_ROOT="${HIVE_ATLAS_SYSTEMD_ROOT:-/etc/systemd/system}"
VERSION=""
TEST_MODE=0
if [ "${HIVE_ATLAS_DEPLOY_TEST_MODE:-}" = 1 ] && [[ "$INSTALL_ROOT" = /tmp/* ]] && [[ "$PRIVATE_ROOT" = /tmp/* ]] && [[ "$SYSTEMD_ROOT" = /tmp/* ]]; then TEST_MODE=1; fi

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || die "--version requires a value"; VERSION="$2"; shift 2 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "invalid version"
[ "$(id -u)" = 0 ] || [ "$TEST_MODE" = 1 ] || die "run as root"
for tool in curl jq systemctl; do command -v "$tool" >/dev/null || die "$tool is required"; done

if [ -f "$PRIVATE_ROOT/atlas.env" ]; then
  # Operator-owned configuration; never shipped with credentials in releases.
  if [ "$TEST_MODE" = 0 ]; then
    [ ! -L "$PRIVATE_ROOT/atlas.env" ] || die "atlas.env must not be a symlink"
    [ "$(stat -c %u "$PRIVATE_ROOT/atlas.env")" = 0 ] || die "atlas.env must be owned by root"
    [ "$(stat -c %a "$PRIVATE_ROOT/atlas.env")" = 600 ] || die "atlas.env must have mode 600"
  fi
  set -a; . "$PRIVATE_ROOT/atlas.env"; set +a
fi
QDRANT_REST="${ATLAS_QDRANT_REST:-https://127.0.0.1:6333}"
OLLAMA_URL="${ATLAS_OLLAMA_URL:-http://127.0.0.1:11434}"
MODEL="${ATLAS_EMBEDDING_MODEL:-nomic-embed-text}"
COLLECTION_BASE="${ATLAS_COLLECTION:-atlas_signals_v01}"
CA_FILE="${QDRANT_TLS_CA_FILE:-$PRIVATE_ROOT/ca.crt}"
CENTER_URL="${HIVE_CENTER_URL:-https://hive-center.duckdns.org}"
HTTP_ADDR="${ATLAS_HTTP_ADDR:-172.28.0.1:8444}"
HEALTH_URL="${ATLAS_HEALTH_URL:-http://$HTTP_ADDR/healthz}"
[[ "$COLLECTION_BASE" =~ ^[A-Za-z0-9_-]+$ ]] || die "invalid collection name"
for value in "$QDRANT_REST" "$OLLAMA_URL" "$MODEL" "$CENTER_URL" "$HTTP_ADDR"; do
  [[ "$value" != *$'\n'* && "$value" != *'"'* && "$value" != *" "* ]] || die "invalid service environment value"
done

SOURCE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[ -f "$SOURCE_ROOT/REVISION" ] || die "bundle has no REVISION"
REVISION="$(tr -d '\r\n' < "$SOURCE_ROOT/REVISION")"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ ]] || die "invalid bundle revision"
[ -x "$SOURCE_ROOT/bin/hive-atlas" ] || die "bundle has no native Atlas executable"
[ -f "$SOURCE_ROOT/signals/core.json" ] || die "bundle has no library"
[ -f "$PRIVATE_ROOT/admin.key" ] || die "missing Qdrant admin key"
[ -f "$CA_FILE" ] || die "missing Qdrant CA"
"$SOURCE_ROOT/bin/hive-atlas" version --json | jq -e --arg version "$VERSION" --arg revision "$REVISION"   '.version == $version and .revision == $revision and .runtime == "go"' >/dev/null || die "bundle identity differs from version/revision"

TARGET="$INSTALL_ROOT/releases/$VERSION"
CURRENT="$INSTALL_ROOT/current"
PREVIOUS="$(readlink "$CURRENT" 2>/dev/null || true)"
if [ "$PREVIOUS" = "$TARGET" ]; then
  curl --fail --silent --show-error --max-time 5 "$HEALTH_URL" | \
    jq -e --arg version "$VERSION" '.status == "ok" and .version == $version and .signals > 0' >/dev/null || die "active release is unhealthy; publish a new tag to change its configuration"
  printf 'Atlas %s is already active and healthy\n' "$VERSION"
  exit 0
fi
COLLECTION="${COLLECTION_BASE}_${VERSION//[.-]/_}_${REVISION:0:12}"
[ "${#COLLECTION}" -le 128 ] || die "collection name exceeds 128 characters"
install -d -m 0755 "$INSTALL_ROOT/releases" "$SYSTEMD_ROOT"
if [ -e "$TARGET" ]; then
  [ -f "$TARGET/REVISION" ] && [ "$(tr -d '\r\n' < "$TARGET/REVISION")" = "$REVISION" ] || die "version already exists with another revision"
fi
install -d -m 0755 "$TARGET/bin" "$TARGET/deploy" "$TARGET/signals"
install -m 0644 "$SOURCE_ROOT/REVISION" "$TARGET/REVISION"
install -m 0755 "$SOURCE_ROOT/bin/hive-atlas" "$TARGET/bin/hive-atlas"
install -m 0644 "$SOURCE_ROOT/signals/core.json" "$TARGET/signals/core.json"
install -m 0755 "$SOURCE_ROOT/deploy/deploy_server.sh" "$TARGET/deploy/deploy_server.sh"
install -m 0644 "$SOURCE_ROOT/deploy/atlas.env.example" "$TARGET/deploy/atlas.env.example"
install -m 0600 "$CA_FILE" "$TARGET/deploy/ca.crt"

printf 'Publishing library snapshot %s\n' "$COLLECTION"
QDRANT_URL="$QDRANT_REST" QDRANT_API_KEY_FILE="$PRIVATE_ROOT/admin.key" QDRANT_TLS_CA_FILE="$CA_FILE" OLLAMA_URL="$OLLAMA_URL" EMBEDDING_MODEL="$MODEL" ATLAS_COLLECTION="$COLLECTION"   "$TARGET/bin/hive-atlas" push --library "$TARGET/signals/core.json"
QDRANT_URL="$QDRANT_REST" QDRANT_API_KEY_FILE="$PRIVATE_ROOT/admin.key" QDRANT_TLS_CA_FILE="$CA_FILE" OLLAMA_URL="$OLLAMA_URL" EMBEDDING_MODEL="$MODEL" ATLAS_COLLECTION="$COLLECTION"   "$TARGET/bin/hive-atlas" mint-reader --output "$TARGET/deploy/reader.key"
{
  printf 'QDRANT_URL=%s\nATLAS_COLLECTION=%s\nOLLAMA_URL=%s\nEMBEDDING_MODEL=%s\nHIVE_CENTER_URL=%s\nATLAS_HTTP_ADDR=%s\n'     "$QDRANT_REST" "$COLLECTION" "$OLLAMA_URL" "$MODEL" "$CENTER_URL" "$HTTP_ADDR"
} > "$TARGET/deploy/service.env"
chmod 0644 "$TARGET/deploy/service.env"

# Retain the previous unit until the new service passes its health check.
UNIT_BACKUP="$(mktemp)"
HAD_UNIT=0
if [ -f "$SYSTEMD_ROOT/hive-atlas.service" ]; then cp "$SYSTEMD_ROOT/hive-atlas.service" "$UNIT_BACKUP"; HAD_UNIT=1; fi
PROMOTED=0
rollback() {
  status=$?
  trap - ERR
  if [ "$PROMOTED" = 1 ]; then
    if [ -n "$PREVIOUS" ]; then
      ln -s "$PREVIOUS" "$INSTALL_ROOT/.rollback.$$"
      mv -Tf "$INSTALL_ROOT/.rollback.$$" "$CURRENT"
    else
      rm -f -- "$CURRENT"
    fi
    if [ "$HAD_UNIT" = 1 ]; then
      install -m 0644 "$UNIT_BACKUP" "$SYSTEMD_ROOT/hive-atlas.service"
      systemctl daemon-reload
      systemctl restart hive-atlas.service || true
    else
      systemctl stop hive-atlas.service || true
      systemctl disable hive-atlas.service || true
      rm -f -- "$SYSTEMD_ROOT/hive-atlas.service"
      systemctl daemon-reload
    fi
    printf 'Atlas deployment failed; previous release restored\n' >&2
  fi
  rm -f -- "$UNIT_BACKUP"
  exit "$status"
}
trap rollback ERR
trap 'rm -f -- "$UNIT_BACKUP"' EXIT
# Path substitution supports an isolated test/install root without changing the unit contract.
sed "s|/opt/hive-atlas|$INSTALL_ROOT|g" "$SOURCE_ROOT/deploy/hive-atlas.service" > "$SYSTEMD_ROOT/hive-atlas.service"
chmod 0644 "$SYSTEMD_ROOT/hive-atlas.service"
ln -s "$TARGET" "$INSTALL_ROOT/.current.$$"
mv -Tf "$INSTALL_ROOT/.current.$$" "$CURRENT"
PROMOTED=1
systemctl daemon-reload
systemctl enable hive-atlas.service
systemctl restart hive-atlas.service
healthy=0
attempts=30; [ "$TEST_MODE" = 0 ] || attempts=2
for ((i=0; i<attempts; i++)); do
  if curl --fail --silent --show-error --max-time 5 "$HEALTH_URL" |     jq -e --arg version "$VERSION" '.status == "ok" and .version == $version and .signals > 0' >/dev/null; then healthy=1; break; fi
  sleep 1
done
# Use a failing command, so the ERR trap restores both the unit and snapshot.
[ "$healthy" = 1 ]
PROMOTED=0
trap - ERR
install -d -m 0700 "$HISTORY_DIR"
printf '{"version":"%s","revision":"%s","collection":"%s","result":"healthy"}\n'   "$VERSION" "$REVISION" "$COLLECTION" >> "$HISTORY_DIR/history.jsonl"
printf 'Atlas %s deployed with remote MCP and immutable library snapshot\n' "$VERSION"
