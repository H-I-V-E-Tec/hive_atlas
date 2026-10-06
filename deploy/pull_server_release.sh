#!/usr/bin/env bash
# Bootstrap root-owned: baixa, autentica e aplica uma release da biblioteca Atlas.
# Espelha deploy/pull_server_release.sh do hive_mind. Instalar como
# /usr/local/sbin/hive-atlas-pull-release (root:root 0755).
set -Eeuo pipefail
umask 077

REPOSITORY="${HIVE_ATLAS_GITHUB_REPOSITORY:-H-I-V-E-Tec/hive_atlas}"
TOKEN_FILE="${HIVE_ATLAS_GITHUB_TOKEN_FILE:-/srv/hive-private/github-release.token}"
VERSION="${1:-}"

die() { printf 'ERRO: %s\n' "$*" >&2; exit 1; }
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "uso: hive-atlas-pull-release vX.Y.Z"
[ "$(id -u)" -eq 0 ] || die "execute como root"
for command in curl cosign sha256sum tar; do
  command -v "$command" >/dev/null || die "$command não encontrado"
done

TEMPORARY="$(mktemp -d /tmp/hive-atlas-release.XXXXXX)"
cleanup() { rm -rf -- "$TEMPORARY"; }
trap cleanup EXIT

CURL_CONFIG="$TEMPORARY/curl.conf"
printf '%s\n' 'fail' 'location' 'silent' 'show-error' 'proto = "=https"' 'tlsv1.2' > "$CURL_CONFIG"
if [ -f "$TOKEN_FILE" ]; then
  [ ! -L "$TOKEN_FILE" ] || die "token GitHub não pode ser link simbólico"
  TOKEN_MODE="$(stat -c '%a' "$TOKEN_FILE")"
  [[ "$TOKEN_MODE" =~ ^[0-7]{3,4}$ ]] || die "modo do token GitHub inválido"
  (( (8#$TOKEN_MODE & 8#77) == 0 )) || die "token GitHub deve ser privado"
  TOKEN="$(tr -d '\r\n' < "$TOKEN_FILE")"
  [[ "$TOKEN" =~ ^[A-Za-z0-9_]+$ ]] || die "token GitHub inválido"
  printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" >> "$CURL_CONFIG"
  unset TOKEN
fi

BASE="https://github.com/$REPOSITORY/releases/download/$VERSION"
ASSET="atlas-server-$VERSION.tar.gz"
for file in "$ASSET" SHA256SUMS SHA256SUMS.sigstore.json; do
  curl --config "$CURL_CONFIG" --output "$TEMPORARY/$file" "$BASE/$file"
done

cosign verify-blob \
  --bundle "$TEMPORARY/SHA256SUMS.sigstore.json" \
  --certificate-identity "https://github.com/$REPOSITORY/.github/workflows/release.yml@refs/tags/$VERSION" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  "$TEMPORARY/SHA256SUMS" >/dev/null

(cd "$TEMPORARY" && grep -F "  $ASSET" SHA256SUMS | sha256sum -c -)
install -d -m 0700 "$TEMPORARY/extracted"
tar -xzf "$TEMPORARY/$ASSET" -C "$TEMPORARY/extracted"
[ -x "$TEMPORARY/extracted/deploy/deploy_server.sh" ] || die "bundle Atlas inválido"
[ -f "$TEMPORARY/extracted/signals/core.json" ] || die "bundle sem biblioteca de sinais"

"$TEMPORARY/extracted/deploy/deploy_server.sh" --version "$VERSION"
