#!/usr/bin/env bash
# provision_qdrant.sh — cria a collection transversal do Atlas e emite tokens.
#
# Espelha guias/CONFIGURAR_QDRANT_E_CODEX.md §3–5, mas para a collection do Atlas
# (sem program_id, transversal). Reusa o MESMO Qdrant/Ollama do hive_mind.
#
# Pré: docker compose up -d qdrant ollama  +  ollama pull nomic-embed-text
# Requer HIVE_QDRANT_ADMIN_KEY no ambiente (a mesma admin key do hive_mind).
#
#   HIVE_QDRANT_ADMIN_KEY=... scripts/provision_qdrant.sh
set -euo pipefail

QDRANT_REST="${QDRANT_REST:-http://127.0.0.1:6333}"
OLLAMA_URL="${OLLAMA_URL:-http://127.0.0.1:11434}"
EMBEDDING_MODEL="${EMBEDDING_MODEL:-nomic-embed-text}"
ATLAS_COLLECTION="${ATLAS_COLLECTION:-atlas_signals_v01}"

: "${HIVE_QDRANT_ADMIN_KEY:?defina HIVE_QDRANT_ADMIN_KEY (a admin key do hive_mind)}"

echo "1/3 · sondando dimensão do modelo ${EMBEDDING_MODEL}..."
DIM="$(
  curl -fsS "${OLLAMA_URL}/api/embeddings" \
    -H 'Content-Type: application/json' \
    --data "{\"model\":\"${EMBEDDING_MODEL}\",\"prompt\":\"atlas dimension probe\"}" |
  python3 -c 'import json,sys; print(len(json.load(sys.stdin)["embedding"]))'
)"
echo "    dimensão: ${DIM}"

echo "2/3 · criando collection ${ATLAS_COLLECTION} (dense Cosine, transversal)..."
curl -fsS -X PUT "${QDRANT_REST}/collections/${ATLAS_COLLECTION}" \
  -H "api-key: ${HIVE_QDRANT_ADMIN_KEY}" \
  -H 'Content-Type: application/json' \
  --data-raw "{\"vectors\":{\"size\":${DIM},\"distance\":\"Cosine\"}}" |
  python3 -m json.tool

echo "3/3 · emitindo tokens (rw p/ popular, r p/ o MCP)..."
mint_atlas_token() {
  TOKEN_ACCESS="$1" ATLAS_COLLECTION="$ATLAS_COLLECTION" python3 - <<'PY'
import base64, hashlib, hmac, json, os, time
def enc(v):
    if not isinstance(v, bytes):
        v = json.dumps(v, separators=(",", ":"), sort_keys=True).encode()
    return base64.urlsafe_b64encode(v).rstrip(b"=").decode()
header = enc({"alg": "HS256", "typ": "JWT"})
payload = enc({"exp": int(time.time()) + 86400,
               "access": [{"collection": os.environ["ATLAS_COLLECTION"],
                           "access": os.environ["TOKEN_ACCESS"]}]})
msg = f"{header}.{payload}".encode()
sig = enc(hmac.new(os.environ["HIVE_QDRANT_ADMIN_KEY"].encode(), msg, hashlib.sha256).digest())
print(f"{header}.{payload}.{sig}")
PY
}
ATLAS_WRITER_TOKEN="$(mint_atlas_token rw)"
ATLAS_READER_TOKEN="$(mint_atlas_token r)"

cat <<EOF

Pronto. Exporte para popular e para o MCP (NÃO versione os tokens):

  # popular (rw):
  export QDRANT_URL="${QDRANT_REST}"
  export ATLAS_COLLECTION="${ATLAS_COLLECTION}"
  export OLLAMA_URL="${OLLAMA_URL}"
  export EMBEDDING_MODEL="${EMBEDDING_MODEL}"
  export QDRANT_API_KEY="${ATLAS_WRITER_TOKEN}"
  python3 -m atlas.push

  # MCP (r): use este token no reader
  ATLAS_READER_TOKEN=${ATLAS_READER_TOKEN}
EOF
