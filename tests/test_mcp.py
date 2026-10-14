"""MCP (Etapa 6) e feedback (10)."""

import json
from pathlib import Path

import pytest

from atlas.mcp_server import Server
from atlas import feedback

ROOT = Path(__file__).resolve().parent.parent


@pytest.fixture
def server() -> Server:
    return Server()


def _call(server: Server, name: str, args: dict) -> dict:
    resp = server.handle({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                          "params": {"name": name, "arguments": args}})
    assert resp["result"]["isError"] is False
    return json.loads(resp["result"]["content"][0]["text"])


def test_initialize_e_tools_list(server: Server):
    init = server.handle({"jsonrpc": "2.0", "id": 0, "method": "initialize"})
    assert init["result"]["serverInfo"]["name"] == "hive-atlas"
    tl = server.handle({"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    names = {t["name"] for t in tl["result"]["tools"]}
    assert names == {"atlas_observe", "atlas_signals_search", "atlas_feedback"}


def test_observe_oauth_da_c02_untested(server: Server):
    out = _call(server, "atlas_observe", {"evidence": [{
        "kind": "js_finding", "flow": "oauth", "endpoint": "/authorize",
        "value": "GET /authorize?client_id=abc&redirect_uri=https://app/cb&response_type=code",
        "ref": "x"}]})
    ids = {l["id"] for l in out["leads"]}
    assert "C-02" in ids
    assert all(l["state"] == "UNTESTED" for l in out["leads"])


def test_observe_next_nao_da_lead(server: Server):
    out = _call(server, "atlas_observe", {"evidence": [{
        "kind": "nota", "value": "GET /v1/items?next=<cursor> paginacao de lista publica", "ref": "x"}]})
    assert out["leads"] == []


def test_search(server: Server):
    out = _call(server, "atlas_signals_search", {"query": "oauth redirect_uri client_id", "k": 3})
    assert any(h["id"] == "C-02" for h in out)


def test_feedback_grava(server: Server, tmp_path, monkeypatch):
    store = tmp_path / "fb.jsonl"
    monkeypatch.setattr(feedback, "DEFAULT_STORE", store)
    out = _call(server, "atlas_feedback",
                {"signal_id": "C-02", "outcome": "confirmado", "note": "ok"})
    assert out["ok"] is True
    assert len(feedback.load(store)) == 1


def test_metodo_desconhecido(server: Server):
    resp = server.handle({"jsonrpc": "2.0", "id": 9, "method": "foo/bar"})
    assert resp["error"]["code"] == -32601
