"""feedback.py — registro de desfechos revisados (Etapa 10 do plano).

Store append-only (JSONL) para o `atlas_feedback`. O feedback alimenta a revisão
do dojo; NÃO generaliza automaticamente de um programa para todos, e o treino de
modelo só é avaliado se as avaliações mostrarem necessidade que contexto e
exemplos não resolvem.
"""

from __future__ import annotations

import json
import os
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def _default_store() -> Path:
    if os.environ.get("ATLAS_FEEDBACK_FILE"):
        return Path(os.environ["ATLAS_FEEDBACK_FILE"])
    if ROOT.is_dir():
        return ROOT / ".atlas" / "feedback.jsonl"
    hive_home = Path(os.environ.get("HIVE_HOME", str(Path.home() / ".hive")))
    return hive_home / "atlas" / "feedback.jsonl"


DEFAULT_STORE = _default_store()

_OUTCOMES = {"confirmado", "descartado", "inconclusivo"}


@dataclass
class Feedback:
    signal_id: str
    outcome: str                 # confirmado | descartado | inconclusivo
    program_id: str = ""
    note: str = ""
    ts: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat(timespec="seconds"))

    def __post_init__(self):
        if self.outcome not in _OUTCOMES:
            raise ValueError(f"outcome inválido: {self.outcome!r} (esperado {sorted(_OUTCOMES)})")


def record(fb: Feedback, store: str | Path | None = None) -> None:
    p = Path(store if store is not None else DEFAULT_STORE)
    p.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with p.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps(asdict(fb), ensure_ascii=False) + "\n")


def load(store: str | Path | None = None) -> list[Feedback]:
    p = Path(store if store is not None else DEFAULT_STORE)
    if not p.exists():
        return []
    out = []
    for line in p.read_text(encoding="utf-8").splitlines():
        if line.strip():
            out.append(Feedback(**json.loads(line)))
    return out
