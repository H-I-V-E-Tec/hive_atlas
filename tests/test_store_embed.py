"""Store Qdrant + embeddings + push + backend do MCP (Etapa 11).

HTTP mockado: nenhuma rede. Substitui atlas._http.request_json por um Qdrant/Ollama
falso em memória.
"""

from pathlib import Path

import pytest

from atlas import _http
from atlas.embed import OllamaEmbedder
from atlas.ficha import load_library
from atlas.retrieval import SemanticRetriever
from atlas.store import QdrantStore, point_id

ROOT = Path(__file__).resolve().parent.parent
LIB = ROOT / "signals" / "core.json"


class FakeQdrantOllama:
    """Simula os endpoints usados: Ollama /api/embeddings e Qdrant points."""

    def __init__(self):
        self.points: dict[str, dict] = {}
        self.calls: list[tuple] = []

    def __call__(self, method, url, payload=None, headers=None, timeout=30.0):
        self.calls.append((method, url))
        if url.endswith("/api/embeddings"):
            # embedding determinístico simples a partir do prompt
            p = payload["prompt"]
            return {"embedding": [float(len(p) % 7), float(len(p) % 3), 1.0]}
        if url.endswith("/points") and method == "PUT":
            for pt in payload["points"]:
                self.points[pt["id"]] = pt
            return {"result": {"status": "acknowledged"}}
        if url.endswith("/points/search"):
            hits = [{"id": pid, "score": 0.9, "payload": pt["payload"]}
                    for pid, pt in self.points.items()]
            return {"result": hits[: payload.get("limit", 5)]}
        if url.endswith("/points/scroll"):
            pts = [{"id": pid, "payload": pt["payload"]} for pid, pt in self.points.items()]
            return {"result": {"points": pts, "next_page_offset": None}}
        if "/collections/" in url and method == "GET":
            return {"result": {}}          # existe
        if "/collections/" in url and method == "PUT":
            return {"result": True}        # criada
        return {}


@pytest.fixture
def fake(monkeypatch):
    fq = FakeQdrantOllama()
    monkeypatch.setattr(_http, "request_json", fq)
    return fq


def test_embedder(fake):
    vec = OllamaEmbedder().embed("oauth redirect")
    assert isinstance(vec, list) and len(vec) == 3


def test_upsert_search_e_load_library(fake):
    lib = load_library(LIB)
    store = QdrantStore(api_key="tok")
    emb = OllamaEmbedder()
    for f in lib.values():
        store.upsert_ficha(f, emb.embed(f.mechanism))
    # o ponto é idempotente por ficha
    assert point_id("C-02") in fake.points
    # search devolve ids de ficha
    hits = dict(store.search(emb.embed("oauth"), k=10))
    assert "C-02" in hits
    # load_library reconstrói fichas avaliáveis (com conditions)
    rebuilt = store.load_library()
    assert rebuilt["C-02"].requires == ["S-REDIR-01"]
    assert any(c.role == "required" for c in rebuilt["C-02"].conditions)


def test_semantic_retriever(fake):
    store = QdrantStore(api_key="tok")
    emb = OllamaEmbedder()
    for f in load_library(LIB).values():
        store.upsert_ficha(f, emb.embed(f.mechanism))
    r = SemanticRetriever(store, emb)
    ids = {fid for fid, _ in r.search("oauth redirect", k=10)}
    assert "C-02" in ids


def test_push(fake):
    from atlas.push import push
    store = QdrantStore(api_key="tok")
    summary = push(LIB, store=store, embedder=OllamaEmbedder())
    assert summary["pushed"] == 3
    assert len(fake.points) == 3


def test_mcp_backend_fallback_json(monkeypatch):
    monkeypatch.delenv("QDRANT_API_KEY", raising=False)
    from atlas.mcp_server import Server
    assert Server().backend == "json"


def test_mcp_backend_qdrant(fake, monkeypatch):
    monkeypatch.setenv("QDRANT_API_KEY", "tok")
    # popula o fake para load_library devolver fichas
    store = QdrantStore(api_key="tok")
    emb = OllamaEmbedder()
    for f in load_library(LIB).values():
        store.upsert_ficha(f, emb.embed(f.mechanism))
    from atlas.mcp_server import Server
    srv = Server()
    assert srv.backend == "qdrant"
    assert "C-02" in srv.engine.library
