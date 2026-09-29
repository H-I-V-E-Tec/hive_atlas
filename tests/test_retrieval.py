"""Recuperação léxica + pré-filtro do motor (Etapa 5)."""

from pathlib import Path

import pytest

from atlas import Corpus, Engine, load_library
from atlas.retrieval import LexicalRetriever, SemanticRetriever


class _FakeStore:
    def search(self, vector, k):
        return [("C-02", 0.9)]


class _FakeEmbedder:
    def embed(self, text):
        return [0.1, 0.2, 0.3]

LIB = Path(__file__).resolve().parent.parent / "signals" / "core.json"


@pytest.fixture
def library():
    return load_library(LIB)


def test_lexical_recupera_ficha_relevante(library):
    r = LexicalRetriever(library)
    hits = dict(r.search("client_id redirect_uri authorize oauth code", k=5))
    assert "C-02" in hits


def test_prefiltro_preserva_o_lead_correto(library):
    """Com pré-filtro por recuperação, o OAuth ainda deve produzir C-02."""
    eng = Engine(library)
    r = LexicalRetriever(library)
    corpus = Corpus.from_dicts([{
        "kind": "js_finding", "flow": "oauth", "endpoint": "/authorize",
        "value": "GET /authorize?client_id=abc&redirect_uri=https://app/cb&response_type=code",
        "ref": "x",
    }])
    ids = {rec.ficha.id for rec in eng.recommend(corpus, retriever=r, k=3)}
    assert "C-02" in ids


def test_semantic_retriever_compoe_embed_e_store(library):
    r = SemanticRetriever(_FakeStore(), _FakeEmbedder())
    ids = {fid for fid, _ in r.search("oauth redirect", k=5)}
    assert ids == {"C-02"}
