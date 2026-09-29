"""license.py — validação de licença offline (Etapa 9 do plano).

Comercialização: o MCP valida a licença no startup e recusa servir sem ela
(fail-closed). A verificação é OFFLINE (não depende de servidor de ativação).

NOTA de segurança: esta implementação usa HMAC-SHA256 (segredo simétrico) para
ficar em stdlib puro. Em produção a chave de verificação NÃO deve ser o mesmo
segredo que assina — migrar para assinatura ASSIMÉTRICA (Ed25519 via
`cryptography`) para que o cliente verifique sem poder emitir licença. A
interface (`issue`/`verify`) não muda.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import time
from dataclasses import dataclass


class LicenseError(Exception):
    pass


def _b64u(b: bytes) -> str:
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()


def _b64u_dec(s: str) -> bytes:
    pad = "=" * (-len(s) % 4)
    return base64.urlsafe_b64decode(s + pad)


@dataclass
class License:
    subject: str
    exp: int                       # epoch seconds; 0 = sem expiração
    seats: int = 1
    features: tuple[str, ...] = ()

    def valid_now(self, now: int | None = None) -> bool:
        now = now if now is not None else int(time.time())
        return self.exp == 0 or now < self.exp


def issue(payload: dict, key: bytes) -> str:
    """Emite um token de licença. Uso de dev/emissor; não embarcar a chave."""
    body = _b64u(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode())
    sig = _b64u(hmac.new(key, body.encode(), hashlib.sha256).digest())
    return f"{body}.{sig}"


def verify(token: str, key: bytes, now: int | None = None) -> License:
    """Verifica assinatura e expiração. Levanta LicenseError se inválida."""
    try:
        body, sig = token.strip().split(".", 1)
    except ValueError:
        raise LicenseError("formato de licença inválido")
    expected = _b64u(hmac.new(key, body.encode(), hashlib.sha256).digest())
    if not hmac.compare_digest(sig, expected):
        raise LicenseError("assinatura de licença inválida")
    payload = json.loads(_b64u_dec(body))
    lic = License(
        subject=payload.get("sub", ""),
        exp=int(payload.get("exp", 0)),
        seats=int(payload.get("seats", 1)),
        features=tuple(payload.get("features", [])),
    )
    if not lic.valid_now(now):
        raise LicenseError("licença expirada")
    return lic


def check_startup() -> License | None:
    """Chamado no startup do MCP. Fail-closed quando exigida.

    Env:
      ATLAS_REQUIRE_LICENSE=1   → exige licença válida (modo comercial)
      ATLAS_LICENSE=<token>     → o token
      ATLAS_LICENSE_KEY=<hex>   → chave de verificação (em produção: chave pública)
    """
    required = os.environ.get("ATLAS_REQUIRE_LICENSE") == "1"
    token = os.environ.get("ATLAS_LICENSE", "")
    key_hex = os.environ.get("ATLAS_LICENSE_KEY", "")
    if not required:
        return None  # modo interno/equipe: acesso pela credencial de leitor da Hive
    if not token or not key_hex:
        raise LicenseError("licença exigida (ATLAS_REQUIRE_LICENSE=1) mas ausente")
    return verify(token, bytes.fromhex(key_hex))
