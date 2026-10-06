"""_http.py — cliente HTTP mínimo (stdlib urllib), sem dependência nova.

Isolado num módulo só para os testes poderem substituir `request_json` por um
transporte falso e rodar offline (sem Qdrant/Ollama vivos).
"""

from __future__ import annotations

import json
import ssl
import urllib.error
import urllib.request


class HttpError(Exception):
    def __init__(self, status: int, body: str):
        self.status = status
        self.body = body
        super().__init__(f"HTTP {status}: {body[:200]}")


def request_json(method: str, url: str, payload: dict | None = None,
                 headers: dict | None = None, timeout: float = 30.0,
                 ca_file: str | None = None) -> dict:
    """Faz uma requisição JSON e devolve o corpo decodificado (ou {} se vazio).

    `ca_file` aponta a CA privada usada pelo Qdrant do servidor (loopback HTTPS):
    sem ele, o contexto TLS padrão do sistema é usado (o normal em dev/http)."""
    data = json.dumps(payload).encode() if payload is not None else None
    hdrs = {"Content-Type": "application/json"}
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(url, data=data, headers=hdrs, method=method)
    context: ssl.SSLContext | None = None
    if ca_file and url.lower().startswith("https"):
        context = ssl.create_default_context(cafile=ca_file)
        context.minimum_version = ssl.TLSVersion.TLSv1_2
    try:
        with urllib.request.urlopen(req, timeout=timeout, context=context) as resp:
            raw = resp.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", "replace") if exc.fp else ""
        raise HttpError(exc.code, body) from exc
    return json.loads(raw) if raw.strip() else {}
