#!/usr/bin/env bash
# Teste do deploy_server.sh sem Qdrant/Ollama reais: curl e o push são falsos.
# Verifica o fluxo (probe → ensure collection → push → count → promover current).
set -Eeuo pipefail
umask 077

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/hive-atlas-deploy-test.XXXXXX")"
cleanup() { rm -rf -- "$TEST_ROOT"; }
trap cleanup EXIT

BUNDLE="$TEST_ROOT/bundle"
INSTALL_ROOT="$TEST_ROOT/install"
PRIVATE_ROOT="$TEST_ROOT/private"
FAKE_BIN="$TEST_ROOT/fake-bin"
CURL_LOG="$TEST_ROOT/curl.log"
PUSH_LOG="$TEST_ROOT/push.log"
mkdir -p "$BUNDLE/deploy" "$BUNDLE/signals" "$PRIVATE_ROOT" "$FAKE_BIN"

# Bundle sintético: scripts e biblioteca reais, REVISION de 40 hex, pacote atlas.
cp "$PROJECT_ROOT/deploy/deploy_server.sh" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/deploy/atlas.env.example" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/signals/core.json" "$BUNDLE/signals/"
cp -a "$PROJECT_ROOT/atlas" "$BUNDLE/atlas"
printf '%040d\n' 1 > "$BUNDLE/REVISION"

# Credenciais mínimas (curl é falso, então o conteúdo não importa).
printf 'admin-key-de-teste-0123456789abcdef\n' > "$PRIVATE_ROOT/admin.key"
: > "$PRIVATE_ROOT/ca.crt"

# curl falso: responde aos três endpoints que o deploy usa.
cat > "$FAKE_BIN/curl" <<'SH'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >> "$HIVE_TEST_CURL_LOG"
url=""; want_code=0
for a in "$@"; do
  case "$a" in http*|https*) url="$a" ;; -w) want_code=1 ;; esac
done
case "$url" in
  *"/api/embeddings"*) echo '{"embedding":[0,0,0,0]}'; exit 0 ;;
  *"/points/count"*)   echo '{"result":{"count":3}}'; exit 0 ;;
esac
if [ "$want_code" = 1 ]; then echo 404; exit 0; fi   # GET collection → não existe
exit 0                                               # PUT create → ok
SH
chmod 0755 "$FAKE_BIN/curl"

# push falso: só registra que foi chamado.
cat > "$FAKE_BIN/atlas-push-stub" <<SH
#!/usr/bin/env bash
printf 'push %s\n' "\$*" >> "$PUSH_LOG"
exit 0
SH
chmod 0755 "$FAKE_BIN/atlas-push-stub"

export HIVE_TEST_CURL_LOG="$CURL_LOG"
: > "$CURL_LOG"; : > "$PUSH_LOG"

PATH="$FAKE_BIN:$PATH" \
HIVE_ATLAS_DEPLOY_TEST_MODE=1 \
HIVE_ATLAS_INSTALL_ROOT="$INSTALL_ROOT" \
HIVE_ATLAS_PRIVATE_ROOT="$PRIVATE_ROOT" \
HIVE_ATLAS_HISTORY_DIR="$TEST_ROOT/history" \
QDRANT_TLS_CA_FILE="$PRIVATE_ROOT/ca.crt" \
ATLAS_PUSH_CMD="atlas-push-stub" \
  bash "$BUNDLE/deploy/deploy_server.sh" --version v9.9.9

fail() { printf 'FALHA: %s\n' "$*" >&2; exit 1; }

# A release foi instalada e promovida.
[ -L "$INSTALL_ROOT/current" ] || fail "current não é symlink"
[ "$(readlink "$INSTALL_ROOT/current")" = "$INSTALL_ROOT/releases/v9.9.9" ] || fail "current não aponta a release"
[ -f "$INSTALL_ROOT/releases/v9.9.9/signals/core.json" ] || fail "biblioteca não instalada"
[ -d "$INSTALL_ROOT/releases/v9.9.9/atlas" ] || fail "pacote atlas não instalado"

# O push foi chamado uma vez.
[ "$(wc -l < "$PUSH_LOG")" -eq 1 ] || fail "push não foi chamado exatamente uma vez"

# A collection foi criada (PUT após o 404) e a contagem foi consultada.
grep -q 'api/embeddings' "$CURL_LOG" || fail "não sondou a dimensão"
grep -q 'points/count' "$CURL_LOG" || fail "não verificou a contagem"
grep -q -- '-X PUT' "$CURL_LOG" || fail "não criou a collection"

# O histórico registrou o deploy saudável.
grep -q '"result":"healthy"' "$TEST_ROOT/history/history.jsonl" || fail "histórico não registrado"

printf 'OK: deploy_server.sh passou no fluxo sintético\n'
