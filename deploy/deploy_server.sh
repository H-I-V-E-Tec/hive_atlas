#!/usr/bin/env bash
# Instala um bundle do Atlas já verificado pelo pull_server_release.sh e publica
# a biblioteca de sinais no Qdrant do servidor (o MESMO do hive_mind): garante a
# collection transversal e roda `atlas.push`. Não sobe serviço nem toca o Mind.
set -Eeuo pipefail
umask 077

INSTALL_ROOT="${HIVE_ATLAS_INSTALL_ROOT:-/opt/hive-atlas}"
PRIVATE_ROOT="${HIVE_ATLAS_PRIVATE_ROOT:-/srv/hive-private}"
HISTORY_DIR="${HIVE_ATLAS_HISTORY_DIR:-/var/lib/hive-atlas-deploy}"
QDRANT_REST="${ATLAS_QDRANT_REST:-https://127.0.0.1:6333}"
OLLAMA_URL="${ATLAS_OLLAMA_URL:-http://127.0.0.1:11434}"
EMBEDDING_MODEL="${ATLAS_EMBEDDING_MODEL:-nomic-embed-text}"
ATLAS_COLLECTION="${ATLAS_COLLECTION:-atlas_signals_v01}"
QDRANT_TLS_CA_FILE="${QDRANT_TLS_CA_FILE:-$PRIVATE_ROOT/ca.crt}"
PYTHON="${ATLAS_PYTHON:-python3}"
VERSION=""

die() { printf 'ERRO: %s\n' "$*" >&2; exit 1; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || die "--version exige valor"; VERSION="$2"; shift 2 ;;
    *) die "argumento desconhecido: $1" ;;
  esac
done

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "versão inválida"
if [ "$(id -u)" -ne 0 ]; then
  if [ "${HIVE_ATLAS_DEPLOY_TEST_MODE:-}" != 1 ] || [[ "$INSTALL_ROOT" != /tmp/* ]] || [[ "$PRIVATE_ROOT" != /tmp/* ]]; then
    die "execute como root"
  fi
fi
command -v "$PYTHON" >/dev/null || die "$PYTHON não encontrado"
command -v curl >/dev/null || die "curl não encontrado"

SOURCE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[ -f "$SOURCE_ROOT/REVISION" ] || die "bundle sem REVISION"
REVISION="$(tr -d '\r\n' < "$SOURCE_ROOT/REVISION")"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ ]] || die "revisão inválida no bundle"
[ -f "$SOURCE_ROOT/signals/core.json" ] || die "bundle sem biblioteca de sinais"
[ -d "$SOURCE_ROOT/atlas" ] || die "bundle sem o pacote atlas"

# Overrides opcionais do operador (CA, urls, modelo, collection). Arquivo privado
# root-only, no formato KEY=value; ausente em dev.
if [ -f "$PRIVATE_ROOT/atlas.env" ]; then
  # shellcheck disable=SC1091
  set -a; . "$PRIVATE_ROOT/atlas.env"; set +a
fi

[ -f "$PRIVATE_ROOT/admin.key" ] || die "falta $PRIVATE_ROOT/admin.key (a admin key do hive_mind)"
[ -f "$QDRANT_TLS_CA_FILE" ] || die "falta a CA do Qdrant: $QDRANT_TLS_CA_FILE"

# ---- instalação do bundle (releases/<versão> + symlink current) ----
TARGET="$INSTALL_ROOT/releases/$VERSION"
CURRENT_LINK="$INSTALL_ROOT/current"
install -d -m 0755 "$INSTALL_ROOT" "$INSTALL_ROOT/releases"
if [ -e "$TARGET" ]; then
  [ -f "$TARGET/REVISION" ] || die "release existente incompleta: $TARGET"
  [ "$(tr -d '\r\n' < "$TARGET/REVISION")" = "$REVISION" ] || die "versão já existe com outra revisão"
  chmod 0755 "$TARGET"
else
  install -d -m 0755 "$TARGET" "$TARGET/deploy" "$TARGET/signals" "$TARGET/scripts"
  install -m 0644 "$SOURCE_ROOT/REVISION" "$TARGET/REVISION"
  install -m 0755 "$SOURCE_ROOT/deploy/deploy_server.sh" "$TARGET/deploy/deploy_server.sh"
  install -m 0644 "$SOURCE_ROOT/signals/core.json" "$TARGET/signals/core.json"
  [ -f "$SOURCE_ROOT/deploy/atlas.env.example" ] && \
    install -m 0644 "$SOURCE_ROOT/deploy/atlas.env.example" "$TARGET/deploy/atlas.env.example"
  [ -f "$SOURCE_ROOT/scripts/provision_qdrant.sh" ] && \
    install -m 0755 "$SOURCE_ROOT/scripts/provision_qdrant.sh" "$TARGET/scripts/provision_qdrant.sh"
  cp -a "$SOURCE_ROOT/atlas" "$TARGET/atlas"
  chmod -R a+rX "$TARGET/atlas"
fi

# ---- credencial do Qdrant (admin key como api-key, sem vazar em ps) ----
ADMIN_KEY="$(tr -d '\r\n' < "$PRIVATE_ROOT/admin.key")"
[ -n "$ADMIN_KEY" ] || die "admin key vazia"
QCONF="$(mktemp)"
trap 'rm -f -- "$QCONF"' EXIT
chmod 0600 "$QCONF"
{
  printf '%s\n' 'silent' 'show-error' 'fail-with-body'
  printf 'cacert = "%s"\n' "$QDRANT_TLS_CA_FILE"
  printf 'header = "api-key: %s"\n' "$ADMIN_KEY"
  printf 'header = "Content-Type: application/json"\n'
} > "$QCONF"

# ---- 1/4 · dimensão do modelo (Ollama local, http) ----
printf '1/4 · sondando dimensão de %s\n' "$EMBEDDING_MODEL"
PROBE="$(curl -sS --max-time 30 "$OLLAMA_URL/api/embeddings" \
  -H 'Content-Type: application/json' \
  --data "{\"model\":\"$EMBEDDING_MODEL\",\"prompt\":\"atlas dimension probe\"}")" \
  || die "Ollama não respondeu em $OLLAMA_URL"
DIM="$(printf '%s' "$PROBE" | "$PYTHON" -c 'import json,sys; print(len(json.load(sys.stdin)["embedding"]))')" \
  || die "resposta de embedding inválida do Ollama"
[[ "$DIM" =~ ^[1-9][0-9]*$ ]] || die "dimensão inválida: $DIM"
printf '    dimensão: %s\n' "$DIM"

# ---- 2/4 · garantir a collection transversal ----
printf '2/4 · garantindo a collection %s\n' "$ATLAS_COLLECTION"
CODE="$(curl -sS --config "$QCONF" -o /dev/null -w '%{http_code}' \
  "$QDRANT_REST/collections/$ATLAS_COLLECTION" || true)"
if [ "$CODE" = "200" ]; then
  printf '    já existe\n'
elif [ "$CODE" = "404" ]; then
  curl -sS --config "$QCONF" -X PUT "$QDRANT_REST/collections/$ATLAS_COLLECTION" \
    --data-raw "{\"vectors\":{\"size\":$DIM,\"distance\":\"Cosine\"}}" >/dev/null \
    || die "falha ao criar a collection"
  printf '    criada (dim=%s)\n' "$DIM"
else
  die "Qdrant respondeu HTTP $CODE ao consultar a collection"
fi

# ---- 3/4 · popular a biblioteca ----
# ATLAS_PUSH_CMD substitui o comando de push (usado só pelo harness de teste).
printf '3/4 · populando a biblioteca (atlas.push)\n'
PUSH_CMD=("$PYTHON" -m atlas.push)
[ -n "${ATLAS_PUSH_CMD:-}" ] && read -r -a PUSH_CMD <<< "$ATLAS_PUSH_CMD"
(
  cd "$TARGET"
  QDRANT_URL="$QDRANT_REST" \
  ATLAS_COLLECTION="$ATLAS_COLLECTION" \
  OLLAMA_URL="$OLLAMA_URL" \
  EMBEDDING_MODEL="$EMBEDDING_MODEL" \
  QDRANT_API_KEY="$ADMIN_KEY" \
  QDRANT_TLS_CA_FILE="$QDRANT_TLS_CA_FILE" \
  "${PUSH_CMD[@]}"
) || die "atlas.push falhou"

# ---- 4/4 · verificação (a collection tem pontos) ----
printf '4/4 · verificando pontos na collection\n'
COUNT_JSON="$(curl -sS --config "$QCONF" -X POST \
  "$QDRANT_REST/collections/$ATLAS_COLLECTION/points/count" \
  --data-raw '{"exact":true}')" || die "falha ao contar pontos"
COUNT="$(printf '%s' "$COUNT_JSON" | "$PYTHON" -c 'import json,sys; print(json.load(sys.stdin)["result"]["count"])')" \
  || die "resposta de contagem inválida"
[[ "$COUNT" =~ ^[0-9]+$ ]] && [ "$COUNT" -gt 0 ] || die "collection vazia após o push (count=$COUNT)"
printf '    pontos: %s\n' "$COUNT"

# ---- promover a release ativa ----
link_tmp="$INSTALL_ROOT/.current.$$"
rm -f -- "$link_tmp"
ln -s "$TARGET" "$link_tmp"
mv -Tf "$link_tmp" "$CURRENT_LINK"

install -d -m 0700 "$HISTORY_DIR"
printf '{"version":"%s","revision":"%s","collection":"%s","points":%s,"result":"healthy"}\n' \
  "$VERSION" "$REVISION" "$ATLAS_COLLECTION" "$COUNT" >> "$HISTORY_DIR/history.jsonl"
printf 'Deploy concluído: %s (%s) · %s ponto(s)\n' "$VERSION" "$REVISION" "$COUNT"
