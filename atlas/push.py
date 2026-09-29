"""push.py — popular a biblioteca no Qdrant (Etapa 11, "começar a popular").

Lê as fichas de `signals/*.json` (a biblioteca revisada, fonte do push), roda a
checagem de confidencialidade dos portões de ingestão, embeda cada ficha
(Ollama) e faz upsert na collection transversal do Atlas.

Env: QDRANT_URL, QDRANT_API_KEY, ATLAS_COLLECTION, OLLAMA_URL, EMBEDDING_MODEL.

Uso:
  python3 -m atlas.push                       # usa signals/core.json
  python3 -m atlas.push --library outra.json
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

from .embed import OllamaEmbedder, ficha_text
from .ficha import load_library
from .ingest import _check_confidentiality
from .store import QdrantStore, _ficha_to_payload

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_LIB = ROOT / "signals" / "core.json"


def push(library_path: Path = DEFAULT_LIB,
         store: QdrantStore | None = None,
         embedder: OllamaEmbedder | None = None) -> dict:
    lib = load_library(library_path)
    store = store or QdrantStore(api_key=os.environ.get("QDRANT_API_KEY", ""))
    embedder = embedder or OllamaEmbedder()

    # confidencialidade antes de subir para o ativo compartilhado (fail-closed).
    leaks: list[str] = []
    for f in lib.values():
        leaks += _check_confidentiality(_ficha_to_payload(f))
    if leaks:
        raise ValueError("push abortado por confidencialidade:\n  " + "\n  ".join(leaks))

    if not store.collection_exists():
        store.create_collection(embedder.dimension())

    for f in lib.values():
        store.upsert_ficha(f, embedder.embed(ficha_text(f)))

    return {"pushed": len(lib), "collection": store.collection}


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description="Popula a biblioteca do Atlas no Qdrant")
    ap.add_argument("--library", default=str(DEFAULT_LIB))
    a = ap.parse_args(argv)
    try:
        summary = push(Path(a.library))
    except Exception as exc:
        print(f"atlas push: {exc}", file=sys.stderr)
        return 1
    print(f"ok: {summary['pushed']} ficha(s) → collection {summary['collection']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
