#!/usr/bin/env bash
# Exercita o bootstrap com uma release privada sintética; não usa rede nem root.
set -Eeuo pipefail
umask 077

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/hive-atlas-pull-test.XXXXXX")"
cleanup() { rm -rf -- "$TEST_ROOT"; }
trap cleanup EXIT
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/fixture" "$TEST_ROOT/package/deploy" "$TEST_ROOT/package/signals"

cat > "$TEST_ROOT/package/deploy/deploy_server.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
test "$1" = --version
test "$2" = v1.2.3
printf 'installed\n' > "$HIVE_TEST_INSTALLED"
SH
chmod 0755 "$TEST_ROOT/package/deploy/deploy_server.sh"
printf '{}\n' > "$TEST_ROOT/package/signals/core.json"
tar -czf "$TEST_ROOT/fixture/atlas-server-v1.2.3.tar.gz" -C "$TEST_ROOT/package" deploy signals
(cd "$TEST_ROOT/fixture" && sha256sum atlas-server-v1.2.3.tar.gz > SHA256SUMS)
printf '{}\n' > "$TEST_ROOT/fixture/SHA256SUMS.sigstore.json"
cat > "$TEST_ROOT/fixture/release.original.json" <<'JSON'
{
  "tag_name": "v1.2.3",
  "draft": false,
  "assets": [
    {"name": "atlas-server-v1.2.3.tar.gz", "state": "uploaded", "id": 101},
    {"name": "SHA256SUMS", "state": "uploaded", "id": 102},
    {"name": "SHA256SUMS.sigstore.json", "state": "uploaded", "id": 103}
  ]
}
JSON
cp "$TEST_ROOT/fixture/release.original.json" "$TEST_ROOT/fixture/release.json"
printf 'test_token_123\n' > "$TEST_ROOT/token"
chmod 0600 "$TEST_ROOT/token"

cat > "$TEST_ROOT/bin/id" <<'SH'
#!/usr/bin/env bash
test "$1" = -u
printf '0\n'
SH
cat > "$TEST_ROOT/bin/cosign" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
test "$1" = verify-blob
case " $* " in
  *" --certificate-identity https://github.com/H-I-V-E-Tec/hive_atlas/.github/workflows/release.yml@refs/tags/v1.2.3 "*) ;;
  *) exit 2 ;;
esac
case " $* " in
  *" --certificate-oidc-issuer https://token.actions.githubusercontent.com "*) ;;
  *) exit 2 ;;
esac
printf 'verify\n' >> "$HIVE_TEST_COSIGN_LOG"
if [ "${HIVE_TEST_SIGNATURE_FAIL:-0}" = 1 ]; then
  printf 'signature rejected\n' >&2
  exit 1
fi
SH
cat > "$TEST_ROOT/bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
config=''; output=''; accept=''; url=''
while (($#)); do
  case "$1" in
    --config) config="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    --header) accept="$2"; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) exit 2 ;;
  esac
done
printf '%s\n' "$url" >> "$HIVE_TEST_CURL_LOG"
grep -Fq 'proto-redir = "=https"' "$config"
if ! grep -Fq 'header = "Authorization: Bearer test_token_123"' "$config"; then
  printf 'curl: (22) The requested URL returned error: 404\n' >&2
  exit 22
fi
case "$url:$accept" in
  https://api.github.com/repos/H-I-V-E-Tec/hive_atlas/releases/tags/v1.2.3:Accept:\ application/vnd.github+json)
    cp "$HIVE_TEST_FIXTURE/release.json" "$output" ;;
  https://api.github.com/repos/H-I-V-E-Tec/hive_atlas/releases/assets/101:Accept:\ application/octet-stream)
    test "${HIVE_TEST_ASSET_FAIL:-0}" != 1 || exit 22
    cp "$HIVE_TEST_FIXTURE/atlas-server-v1.2.3.tar.gz" "$output" ;;
  https://api.github.com/repos/H-I-V-E-Tec/hive_atlas/releases/assets/102:Accept:\ application/octet-stream)
    cp "$HIVE_TEST_FIXTURE/SHA256SUMS" "$output" ;;
  https://api.github.com/repos/H-I-V-E-Tec/hive_atlas/releases/assets/103:Accept:\ application/octet-stream)
    cp "$HIVE_TEST_FIXTURE/SHA256SUMS.sigstore.json" "$output" ;;
  *) exit 3 ;;
esac
SH
chmod 0755 "$TEST_ROOT/bin/"*
export HIVE_TEST_FIXTURE="$TEST_ROOT/fixture"
export HIVE_TEST_CURL_LOG="$TEST_ROOT/curl.log"
export HIVE_TEST_COSIGN_LOG="$TEST_ROOT/cosign.log"
export HIVE_TEST_INSTALLED="$TEST_ROOT/installed"
export HIVE_ATLAS_GITHUB_REPOSITORY=H-I-V-E-Tec/hive_atlas
export HIVE_ATLAS_GITHUB_TOKEN_FILE="$TEST_ROOT/token"
: > "$HIVE_TEST_CURL_LOG"
: > "$HIVE_TEST_COSIGN_LOG"

run_pull() {
  PATH="$TEST_ROOT/bin:$PATH" bash "$PROJECT_ROOT/deploy/pull_server_release.sh" v1.2.3
}
fail() { printf 'FALHA: %s\n' "$*" >&2; exit 1; }
expect_failure() {
  local name="$1" message="$2"
  HIVE_TEST_INSTALLED="$TEST_ROOT/unexpected-$name" \
    run_pull > "$TEST_ROOT/$name.log" 2>&1 && fail "$name deveria impedir o deploy"
  [ ! -e "$TEST_ROOT/unexpected-$name" ] || fail "$name chamou o deploy"
  grep -Fq "$message" "$TEST_ROOT/$name.log" || fail "$name não explicou a falha"
}

# O curl falso só permite os endpoints autenticados da API; a URL antiga falha.
run_pull > "$TEST_ROOT/success.log" 2>&1
[ "$(cat "$HIVE_TEST_INSTALLED")" = installed ] || fail "deploy não foi chamado"
[ "$(wc -l < "$HIVE_TEST_CURL_LOG")" -eq 4 ] || fail "esperava consulta da release e três assets"
[ "$(wc -l < "$HIVE_TEST_COSIGN_LOG")" -eq 1 ] || fail "assinatura não foi conferida"
grep -Fq 'atlas-server-v1.2.3.tar.gz: OK' "$TEST_ROOT/success.log" || fail "checksum não foi conferido"
grep -Fq test_token_123 "$TEST_ROOT/success.log" && fail "token apareceu no log"

HIVE_ATLAS_GITHUB_TOKEN_FILE="$TEST_ROOT/missing-token" \
  expect_failure missing-token 'confira a tag e o token GitHub'
chmod 0644 "$TEST_ROOT/token"
expect_failure token-mode 'token GitHub deve ser privado'
chmod 0600 "$TEST_ROOT/token"

jq '.tag_name = "v9.9.9"' "$TEST_ROOT/fixture/release.original.json" > "$TEST_ROOT/fixture/release.json"
expect_failure wrong-tag 'metadados inválidos'
jq '.draft = true' "$TEST_ROOT/fixture/release.original.json" > "$TEST_ROOT/fixture/release.json"
expect_failure draft 'metadados inválidos'
jq '.assets |= map(select(.id != 103))' "$TEST_ROOT/fixture/release.original.json" > "$TEST_ROOT/fixture/release.json"
expect_failure missing-asset 'asset SHA256SUMS.sigstore.json ausente ou duplicado'
jq '.assets += [.assets[0]]' "$TEST_ROOT/fixture/release.original.json" > "$TEST_ROOT/fixture/release.json"
expect_failure duplicate-asset 'asset atlas-server-v1.2.3.tar.gz ausente ou duplicado'
jq '.assets[0].id = "../wrong"' "$TEST_ROOT/fixture/release.original.json" > "$TEST_ROOT/fixture/release.json"
expect_failure invalid-id 'ID inválido'
cp "$TEST_ROOT/fixture/release.original.json" "$TEST_ROOT/fixture/release.json"

HIVE_TEST_ASSET_FAIL=1 expect_failure asset-download 'falha ao baixar o asset'
HIVE_TEST_SIGNATURE_FAIL=1 expect_failure signature 'signature rejected'
printf 'tampered\n' >> "$TEST_ROOT/fixture/atlas-server-v1.2.3.tar.gz"
expect_failure checksum 'FAILED'

printf 'OK: pull_server_release.sh passou no fluxo privado e nas falhas de acesso, assinatura e checksum\n'
