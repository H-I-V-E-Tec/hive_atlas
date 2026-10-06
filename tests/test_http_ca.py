"""CA privada do Qdrant TLS (caminho usado pelo deploy no servidor).

O store só deve repassar `ca_file` ao transporte quando ele estiver definido,
para não mudar a chamada em dev/http nem a assinatura do transporte falso.
"""

from atlas import _http
from atlas.store import QdrantStore


def _recorder(monkeypatch):
    calls = []

    def fake(method, url, payload=None, headers=None, timeout=30.0, ca_file=None):
        calls.append({"method": method, "url": url, "ca_file": ca_file})
        if method == "GET":
            return {"result": {}}
        return {}

    monkeypatch.setattr(_http, "request_json", fake)
    return calls


def test_store_sem_ca_nao_passa_ca_file(monkeypatch):
    calls = _recorder(monkeypatch)
    QdrantStore(api_key="tok", ca_file="").collection_exists()
    assert calls[0]["ca_file"] is None


def test_store_com_ca_repassa_ao_transporte(monkeypatch):
    calls = _recorder(monkeypatch)
    QdrantStore(url="https://127.0.0.1:6333", api_key="tok",
                ca_file="/srv/hive-private/ca.crt").collection_exists()
    assert calls[0]["ca_file"] == "/srv/hive-private/ca.crt"
