"""embed.py — embeddings via Ollama (mesmo modelo do hive_mind).

`OllamaEmbedder` fala com o endpoint local do Ollama (`/api/embeddings`) usando o
modelo `nomic-embed-text`, como o hive_mind. Sem dependência nova (usa _http).
"""

from __future__ import annotations

import os

from . import _http
from .ficha import Ficha
from .retrieval import _ficha_doc

DEFAULT_URL = os.environ.get("OLLAMA_URL", "http://127.0.0.1:11434")
DEFAULT_MODEL = os.environ.get("EMBEDDING_MODEL", "nomic-embed-text")


def ficha_text(f: Ficha) -> str:
    """Texto embeddável de uma ficha: o mesmo bag usado na recuperação léxica,
    para que semântica e léxico enxerguem o mesmo conteúdo."""
    return " ".join(sorted(_ficha_doc(f)))


class OllamaEmbedder:
    def __init__(self, url: str = DEFAULT_URL, model: str = DEFAULT_MODEL):
        self.url = url.rstrip("/")
        self.model = model

    def embed(self, text: str) -> list[float]:
        out = _http.request_json(
            "POST", f"{self.url}/api/embeddings",
            payload={"model": self.model, "prompt": text},
        )
        vec = out.get("embedding")
        if not isinstance(vec, list) or not vec:
            raise ValueError("Ollama não retornou embedding")
        return vec

    def dimension(self) -> int:
        return len(self.embed("atlas dimension probe"))
