"""ficha.py — a ficha de sinal e suas condições (ver docs/ficha-schema.md).

Diferença central para o hypotheses.py atual: além do gatilho textual
(`patterns`), a ficha declara CONDIÇÕES verificáveis que o motor checa contra a
evidência. Uma condição resolve para presente / ausente / desconhecida — nunca
silenciosamente presente.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from importlib.abc import Traversable  # Python 3.10 também suporta esse tipo

SCHEMA_VERSION = 1

_STATES = {"candidata", "revisada", "ativa", "arquivada"}
_CONDITION_ROLES = {"required", "refute"}

# Resolução de condição
PRESENTE = "presente"
AUSENTE = "ausente"
DESCONHECIDA = "desconhecida"

# Prioridade por classe (menor = mais alto), herdada do hypotheses.py.
CLASS_WEIGHT = {
    "IDOR": 1, "BAC": 1, "LOGIC": 2, "HMAC": 3, "HDR": 4,
    "REDIR": 5, "OSINT": 6,
}


@dataclass
class Condition:
    """Uma pré-condição verificável do mecanismo.

    role="required": precisa estar `presente` para o mecanismo se aplicar.
    role="refute":   se `presente`, DERRUBA o sinal (marca de contraexemplo).
    """

    id: str
    pattern: str
    means: str
    role: str = "required"

    def __post_init__(self) -> None:
        if self.role not in _CONDITION_ROLES:
            raise ValueError(f"role inválido: {self.role!r}")
        try:
            self._rx = re.compile(self.pattern, re.IGNORECASE)
        except re.error as exc:  # literal fallback
            self._rx = re.compile(re.escape(self.pattern), re.IGNORECASE)

    def resolve(self, haystacks: list[str]) -> str:
        """presente se casa em algum haystack; ausente se há evidência mas não
        casa; desconhecida se não há evidência que permita decidir."""
        if not haystacks:
            return DESCONHECIDA
        for h in haystacks:
            if self._rx.search(h):
                return PRESENTE
        return AUSENTE


@dataclass
class Ficha:
    """Uma ficha de sinal. Campos de prosa são opcionais no núcleo; os campos
    operacionais (patterns/conditions/feeds) dirigem o motor."""

    id: str
    version: int = 1
    state: str = "candidata"
    kind: str = "signal"            # "signal" (S-*) ou "chain" (C-*)
    signal_class: str = ""          # IDOR/BAC/LOGIC/HMAC/HDR/REDIR/OSINT
    mechanism: str = ""
    patterns: list[str] = field(default_factory=list)
    conditions: list[Condition] = field(default_factory=list)
    feeds: list[str] = field(default_factory=list)   # ids de C-* que este S-* alimenta
    requires: list[str] = field(default_factory=list)  # feeders exigidos por um C-*
    confirmation: str = ""
    counterexample: str = ""

    def __post_init__(self) -> None:
        if self.state not in _STATES:
            raise ValueError(f"state inválido: {self.state!r}")
        self._pats = [self._compile(p) for p in self.patterns]

    @staticmethod
    def _compile(raw: str) -> "re.Pattern":
        try:
            return re.compile(raw, re.IGNORECASE)
        except re.error:
            return re.compile(re.escape(raw), re.IGNORECASE)

    def triggers(self, haystacks: list[str]) -> bool:
        """Algum pattern acende sobre a evidência?"""
        if not self._pats:
            return False
        return any(rx.search(h) for rx in self._pats for h in haystacks)

    @property
    def weight(self) -> int:
        return CLASS_WEIGHT.get(self.signal_class, 9)

    @classmethod
    def from_dict(cls, d: dict) -> "Ficha":
        conds = [Condition(**c) for c in d.get("conditions", [])]
        known = {f for f in cls.__dataclass_fields__}  # type: ignore[attr-defined]
        payload = {k: v for k, v in d.items() if k in known and k != "conditions"}
        return cls(conditions=conds, **payload)


def load_library(path: str | Path | Traversable) -> dict[str, Ficha]:
    """Carrega a biblioteca de fichas de um arquivo JSON (loja dependency-light).

    A escolha final da loja (SQLite/Qdrant) é questão em aberto; o motor só
    depende de um dict id → Ficha.
    """
    resource = Path(path) if isinstance(path, str) else path
    data = json.loads(resource.read_text(encoding="utf-8"))
    if data.get("schema_version") != SCHEMA_VERSION:
        raise ValueError(f"schema_version incompatível: {data.get('schema_version')}")
    fichas = [Ficha.from_dict(f) for f in data.get("signals", [])]
    lib: dict[str, Ficha] = {}
    for f in fichas:
        if f.id in lib:
            raise ValueError(f"id duplicado na biblioteca: {f.id}")
        lib[f.id] = f
    return lib
