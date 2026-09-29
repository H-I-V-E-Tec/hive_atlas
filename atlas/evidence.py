"""evidence.py — modelo de evidência da sessão (contrato sessão → Atlas, v1).

Ver docs/contrato-evidencia.md. O Atlas nunca lê o alvo; consome estes eventos,
que descrevem evidência já coletada dentro de escopo autorizado.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Iterable

SCHEMA_VERSION = 1

_KINDS = {"endpoint", "param", "header", "js_finding", "nota"}


@dataclass
class Evidence:
    """Um evento de evidência normalizado."""

    kind: str
    value: str = ""
    program_id: str = ""
    asset: str = ""
    flow: str = ""
    endpoint: str = ""
    method: str = ""
    role: str = ""
    object: str = ""
    ref: str = ""

    def __post_init__(self) -> None:
        if self.kind not in _KINDS:
            raise ValueError(f"kind inválido: {self.kind!r} (esperado {sorted(_KINDS)})")

    @classmethod
    def from_dict(cls, d: dict) -> "Evidence":
        known = {f for f in cls.__dataclass_fields__}  # type: ignore[attr-defined]
        return cls(**{k: v for k, v in d.items() if k in known})

    def haystack(self) -> str:
        """Texto pesquisável do evento (value + campos de roteamento)."""
        parts = [self.value, self.endpoint, self.flow, self.object, self.method, self.role]
        return " ".join(p for p in parts if p)

    def scope_key(self) -> tuple[str, str, str]:
        """Chave de fluxo: sinais de fluxos/ativos diferentes não encadeiam soltos."""
        return (self.program_id, self.asset, self.flow)


@dataclass
class Corpus:
    """Conjunto de evidência de uma sessão (snapshot ou acumulado ao vivo)."""

    events: list[Evidence] = field(default_factory=list)

    @classmethod
    def from_dicts(cls, items: Iterable[dict]) -> "Corpus":
        return cls([Evidence.from_dict(d) for d in items])

    def add(self, ev: Evidence) -> None:
        self.events.append(ev)

    def __len__(self) -> int:
        return len(self.events)

    def __iter__(self):
        return iter(self.events)

    def haystacks(self) -> list[str]:
        return [e.haystack() for e in self.events]
