"""retrieval.py — recuperação de fichas candidatas (Etapa 5 do plano).

A busca seleciona candidatos; o motor decide aplicabilidade sobre os artefatos
concretos. Aqui há uma implementação LÉXICA dependency-light (stdlib) e uma
COSTURA para um backend semântico (embeddings via Ollama + Qdrant, o mesmo stack
do hive_mind) a ser plugado quando houver casos que justifiquem — medindo o ganho
antes, como manda o plano.
"""

from __future__ import annotations

import re
from typing import Protocol

from .ficha import Ficha

_WORD = re.compile(r"[a-z0-9_]{2,}")
# ruído de metacaractere/token de regex que não deve virar termo
_STOP = {"redirect", "b", "s", "d", "w"}  # ajustável; 'b/d/w' vêm de \b \d \w


def _terms(text: str) -> set[str]:
    return {t for t in _WORD.findall(text.lower())}


def _ficha_doc(f: Ficha) -> set[str]:
    """Bag-of-words da ficha: termos dos patterns + mecanismo + contraexemplo."""
    bag: set[str] = set()
    for p in f.patterns:
        bag |= _terms(p)
    bag |= _terms(f.mechanism)
    bag |= _terms(f.counterexample)
    for c in f.conditions:
        bag |= _terms(c.means)
    return bag - _STOP


class Retriever(Protocol):
    def search(self, query: str, k: int) -> list[tuple[str, float]]:
        """Retorna [(ficha_id, score)] ordenado por relevância decrescente."""
        ...


class LexicalRetriever:
    """Sobreposição de termos entre a evidência e o doc da ficha. Sem embeddings.

    Serve de referência e de fallback: mesmo com backend semântico plugado, a
    busca textual continua útil (o plano combina textual + semântica)."""

    def __init__(self, library: dict[str, Ficha]):
        self.library = library
        self._docs = {fid: _ficha_doc(f) for fid, f in library.items()}

    def search(self, query: str, k: int = 5) -> list[tuple[str, float]]:
        q = _terms(query) - _STOP
        scored: list[tuple[str, float]] = []
        for fid, doc in self._docs.items():
            if not doc:
                continue
            overlap = len(q & doc)
            if overlap:
                # normaliza pelo tamanho do doc p/ não favorecer fichas verbosas
                scored.append((fid, overlap / (len(doc) ** 0.5)))
        scored.sort(key=lambda x: (-x[1], x[0]))
        return scored[:k]


class SemanticRetriever:
    """COSTURA para recuperação semântica. Não implementado no núcleo.

    Plano de implementação (quando os casos justificarem, medindo o ganho):
    embeddings locais via Ollama (`nomic-embed-text`, como o hive_mind) sobre o
    doc de cada ficha, indexados num Qdrant próprio do Atlas com contrato
    transversal (sem `program_id`). A interface é a mesma `search`, então o motor
    e o MCP não mudam ao trocar/compor o backend.
    """

    def __init__(self, *_args, **_kwargs):
        raise NotImplementedError(
            "recuperação semântica é etapa futura; use LexicalRetriever até haver "
            "casos que justifiquem embeddings (ver docstring)."
        )

    def search(self, query: str, k: int = 5) -> list[tuple[str, float]]:  # pragma: no cover
        raise NotImplementedError
