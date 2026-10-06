"""store.py — biblioteca de sinais no Qdrant (collection transversal).

`QdrantStore` fala REST (porta 6333) com header `api-key` (JWT escopado só à
collection do Atlas). A collection NÃO tem `program_id`: é transversal. O payload
de cada ponto é a ficha completa; o vetor é o embedding do texto da ficha.

Sem dependência nova (usa _http). O ponto de cada ficha é idempotente
(`uuid5(ficha.id)`), então o push re-roda sem duplicar.
"""

from __future__ import annotations

import os
import uuid

from . import _http
from .ficha import Ficha

DEFAULT_URL = os.environ.get("QDRANT_URL_REST", os.environ.get("QDRANT_URL", "http://127.0.0.1:6333"))
DEFAULT_COLLECTION = os.environ.get("ATLAS_COLLECTION", "atlas_signals_v01")
# CA privada do Qdrant do servidor (loopback HTTPS). Vazio em dev/http.
DEFAULT_CA_FILE = os.environ.get("QDRANT_TLS_CA_FILE", "")
_NS = uuid.UUID("a71a5000-0000-4000-8000-000000000000")  # namespace fixo do Atlas


def point_id(ficha_id: str) -> str:
    return str(uuid.uuid5(_NS, ficha_id))


class QdrantStore:
    def __init__(self, url: str = DEFAULT_URL, api_key: str = "",
                 collection: str = DEFAULT_COLLECTION,
                 ca_file: str = DEFAULT_CA_FILE):
        self.url = url.rstrip("/")
        self.collection = collection
        self.ca_file = ca_file
        self._headers = {"api-key": api_key} if api_key else {}

    def _req(self, method: str, path: str, payload: dict | None = None) -> dict:
        # Só passa ca_file quando definido, para não alterar a chamada em dev/http
        # (e manter o transporte falso dos testes com a mesma assinatura).
        kwargs = {"ca_file": self.ca_file} if self.ca_file else {}
        return _http.request_json(method, f"{self.url}{path}", payload, self._headers, **kwargs)

    # ---- provisionamento (o script provision_qdrant.sh é o caminho normal) ----
    def create_collection(self, dim: int) -> None:
        self._req("PUT", f"/collections/{self.collection}",
                  {"vectors": {"size": dim, "distance": "Cosine"}})

    def collection_exists(self) -> bool:
        try:
            self._req("GET", f"/collections/{self.collection}")
            return True
        except _http.HttpError as exc:
            if exc.status == 404:
                return False
            raise

    # ---- escrita ----
    def upsert_ficha(self, f: Ficha, vector: list[float]) -> None:
        payload = _ficha_to_payload(f)
        self._req("PUT", f"/collections/{self.collection}/points",
                  {"points": [{"id": point_id(f.id), "vector": vector, "payload": payload}]})

    # ---- leitura ----
    def search(self, vector: list[float], k: int = 5) -> list[tuple[str, float]]:
        out = self._req("POST", f"/collections/{self.collection}/points/search",
                        {"vector": vector, "limit": k, "with_payload": True})
        hits = out.get("result", [])
        return [(h["payload"]["id"], float(h["score"])) for h in hits if h.get("payload")]

    def load_library(self) -> dict[str, Ficha]:
        """Scroll de todos os pontos → dict id→Ficha, para o motor avaliar
        condições (o payload carrega patterns/conditions/etc.)."""
        lib: dict[str, Ficha] = {}
        offset = None
        while True:
            body: dict = {"limit": 256, "with_payload": True, "with_vector": False}
            if offset is not None:
                body["offset"] = offset
            out = self._req("POST", f"/collections/{self.collection}/points/scroll", body)
            result = out.get("result", {})
            for pt in result.get("points", []):
                f = Ficha.from_dict(pt["payload"])
                lib[f.id] = f
            offset = result.get("next_page_offset")
            if offset is None:
                break
        return lib


def _ficha_to_payload(f: Ficha) -> dict:
    return {
        "id": f.id, "version": f.version, "state": f.state, "kind": f.kind,
        "signal_class": f.signal_class, "mechanism": f.mechanism,
        "patterns": list(f.patterns),
        "conditions": [{"id": c.id, "pattern": c.pattern, "means": c.means, "role": c.role}
                       for c in f.conditions],
        "feeds": list(f.feeds), "requires": list(f.requires),
        "confirmation": f.confirmation, "counterexample": f.counterexample,
    }
