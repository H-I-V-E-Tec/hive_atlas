"""mcp_server.py — servidor MCP do Atlas (Etapa 6 do plano).

Superfície enxuta sobre o núcleo. JSON-RPC 2.0 por linhas sobre stdio, em stdlib
(sem depender do pacote `mcp`). Reader ≠ writer: a ingestão de fichas (dojo) NÃO
é exposta aqui.

Ferramentas:
  atlas_observe          — evidência da sessão → board de melhor caminho [UNTESTED]
  atlas_signals_search   — recupera fichas candidatas (léxico)
  atlas_feedback         — registra desfecho revisado

Licença: `check_startup` roda em main() e é fail-closed no modo comercial.
"""

from __future__ import annotations

import json
import os
import sys
from importlib import resources
from pathlib import Path

from .engine import Engine
from .evidence import Corpus
from .feedback import Feedback, record
from .ficha import load_library
from .license import LicenseError, check_startup
from .retrieval import LexicalRetriever
from .version import VERSION

ROOT = Path(__file__).resolve().parent.parent
SUPPORTED_PROTOCOL_VERSIONS = ("2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05")
PROTOCOL_VERSION = SUPPORTED_PROTOCOL_VERSIONS[0]

TOOLS = [
    {
        "name": "atlas_observe",
        "description": "Dado a evidência já coletada de uma sessão autorizada, "
                       "devolve o board de melhor caminho (sinais candidatos "
                       "rankeados, sempre [UNTESTED]). Não toca o alvo.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "evidence": {"type": "array", "items": {"type": "object"}},
                "k": {"type": "integer", "default": 0,
                      "description": "orçamento de fichas via recuperação (0 = avaliar todas)"},
            },
            "required": ["evidence"],
        },
    },
    {
        "name": "atlas_signals_search",
        "description": "Recupera fichas candidatas da biblioteca por texto.",
        "inputSchema": {
            "type": "object",
            "properties": {"query": {"type": "string"}, "k": {"type": "integer", "default": 5}},
            "required": ["query"],
        },
    },
    {
        "name": "atlas_feedback",
        "description": "Registra desfecho revisado de uma recomendação "
                       "(confirmado | descartado | inconclusivo).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "signal_id": {"type": "string"},
                "outcome": {"type": "string", "enum": ["confirmado", "descartado", "inconclusivo"]},
                "program_id": {"type": "string"},
                "note": {"type": "string"},
            },
            "required": ["signal_id", "outcome"],
        },
    },
]


def _build_backend(library_path):
    """Escolhe o backend: Qdrant (biblioteca hospedada + semântica) quando
    configurado, senão JSON local + léxico (mantém dev e testes offline).
    Falha no Qdrant cai para o JSON com aviso, em vez de derrubar o servidor."""
    if os.environ.get("QDRANT_API_KEY"):
        try:
            from .store import QdrantStore
            from .embed import OllamaEmbedder
            from .retrieval import SemanticRetriever
            store = QdrantStore(api_key=os.environ["QDRANT_API_KEY"])
            lib = store.load_library()
            if lib:
                return Engine(lib), SemanticRetriever(store, OllamaEmbedder()), "qdrant"
            print("atlas: collection vazia; caindo para biblioteca local", file=sys.stderr)
        except Exception as exc:  # rede/collection ausente → fallback
            print(f"atlas: Qdrant indisponível ({exc}); usando biblioteca local", file=sys.stderr)
    lib = load_library(library_path)
    return Engine(lib), LexicalRetriever(lib), "json"


class Server:
    def __init__(self, library_path: Path | None = None):
        if library_path is None:
            local = ROOT / "signals" / "core.json"
            library_path = local if local.is_file() else resources.files("atlas").joinpath("data/core.json")
        self.engine, self.retriever, self.backend = _build_backend(library_path)

    # ---- tools ----
    def _observe(self, args: dict) -> str:
        corpus = Corpus.from_dicts(args.get("evidence", []))
        k = int(args.get("k", 0))
        leads = self.engine.recommend(corpus, retriever=self.retriever if k else None, k=k or 5)
        board = self.engine.render_board(corpus)
        payload = {
            "board": board,
            "leads": [
                {"id": r.ficha.id, "version": r.ficha.version, "verdict": r.verdict,
                 "present": r.present, "absent": r.absent, "unknown": r.unknown,
                 "state": "UNTESTED"}
                for r in leads
            ],
        }
        return json.dumps(payload, ensure_ascii=False, indent=2)

    def _search(self, args: dict) -> str:
        hits = self.retriever.search(args["query"], int(args.get("k", 5)))
        out = [{"id": fid, "score": round(score, 3),
                "mechanism": self.engine.library[fid].mechanism} for fid, score in hits]
        return json.dumps(out, ensure_ascii=False, indent=2)

    def _feedback(self, args: dict) -> str:
        fb = Feedback(signal_id=args["signal_id"], outcome=args["outcome"],
                      program_id=args.get("program_id", ""), note=args.get("note", ""))
        record(fb)
        return json.dumps({"ok": True, "recorded": fb.ts}, ensure_ascii=False)

    def _call_tool(self, name: str, args: dict) -> str:
        if name == "atlas_observe":
            return self._observe(args)
        if name == "atlas_signals_search":
            return self._search(args)
        if name == "atlas_feedback":
            return self._feedback(args)
        raise ValueError(f"ferramenta desconhecida: {name}")

    # ---- JSON-RPC ----
    def handle(self, req: dict) -> dict | None:
        method = req.get("method")
        rid = req.get("id")
        try:
            if method == "initialize":
                requested = req.get("params", {}).get("protocolVersion")
                result = {
                    "protocolVersion": requested if requested in SUPPORTED_PROTOCOL_VERSIONS else PROTOCOL_VERSION,
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "hive-atlas", "version": VERSION},
                }
            elif method in ("notifications/initialized", "initialized"):
                return None  # notificação, sem resposta
            elif method == "tools/list":
                result = {"tools": TOOLS}
            elif method == "tools/call":
                params = req.get("params", {})
                text = self._call_tool(params.get("name", ""), params.get("arguments", {}))
                result = {"content": [{"type": "text", "text": text}], "isError": False}
            elif method == "ping":
                result = {}
            else:
                return {"jsonrpc": "2.0", "id": rid,
                        "error": {"code": -32601, "message": f"method not found: {method}"}}
        except Exception as exc:  # erro de ferramenta → isError, não derruba o servidor
            if method == "tools/call":
                return {"jsonrpc": "2.0", "id": rid,
                        "result": {"content": [{"type": "text", "text": f"erro: {exc}"}],
                                   "isError": True}}
            return {"jsonrpc": "2.0", "id": rid,
                    "error": {"code": -32603, "message": str(exc)}}
        if rid is None:
            return None
        return {"jsonrpc": "2.0", "id": rid, "result": result}


def main() -> int:
    # JSON-RPC stdio deve usar UTF-8 também em terminais Windows.
    for stream in (sys.stdin, sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    try:
        lic = check_startup()
    except LicenseError as exc:
        print(f"atlas: {exc}", file=sys.stderr)
        return 2
    if lic:
        print(f"atlas: licença ok (sub={lic.subject})", file=sys.stderr)

    server = Server()
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue
        resp = server.handle(req)
        if resp is not None:
            sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
            sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
