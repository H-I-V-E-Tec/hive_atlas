"""watcher.py — observação ao vivo da sessão (Etapa 4 do plano).

Um watcher por polling (stdlib, sem dependências) que tail-eia arquivos de
evidência da sessão (WAL do brain/, recon em targets/), converte linhas em
eventos de evidência (contrato v1) e, a cada novo artefato, reavalia e reescreve
um board de melhor caminho.

"Ao vivo" = o mesmo motor da avaliação em batch, realimentado incrementalmente.
O watcher NUNCA toca o alvo; só lê arquivos locais já produzidos pela caçada.
"""

from __future__ import annotations

import json
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

from .engine import Engine
from .evidence import Corpus, Evidence


@dataclass
class LineAdapter:
    """Converte uma linha de arquivo em um evento de evidência.

    - Linha que é JSON de objeto → Evidence.from_dict (evento já estruturado).
    - Caso contrário → nota de texto cru, com programa/ativo do contexto.
    """

    program_id: str = ""
    asset: str = ""

    def to_event(self, line: str, ref: str) -> Evidence | None:
        s = line.strip()
        if not s:
            return None
        if s[0] == "{":
            try:
                d = json.loads(s)
                d.setdefault("ref", ref)
                d.setdefault("program_id", self.program_id)
                d.setdefault("asset", self.asset)
                return Evidence.from_dict(d)
            except (json.JSONDecodeError, TypeError, ValueError):
                pass
        return Evidence(kind="nota", value=s, ref=ref,
                        program_id=self.program_id, asset=self.asset)


@dataclass
class SessionWatcher:
    engine: Engine
    sources: list[Path]
    board_path: Path
    adapter: LineAdapter = field(default_factory=LineAdapter)
    corpus: Corpus = field(default_factory=Corpus)
    _offsets: dict[Path, int] = field(default_factory=dict)

    def _read_new_lines(self, path: Path) -> list[tuple[str, str]]:
        if not path.exists():
            return []
        data = path.read_text(encoding="utf-8", errors="replace")
        start = self._offsets.get(path, 0)
        chunk = data[start:]
        self._offsets[path] = len(data)
        if not chunk:
            return []
        base_line = data[:start].count("\n")
        out = []
        for i, line in enumerate(chunk.splitlines()):
            out.append((line, f"{path.name}:{base_line + i + 1}"))
        return out

    def poll_once(self) -> int:
        """Lê o que apareceu de novo, atualiza o corpus e reescreve o board.
        Retorna quantos eventos novos entraram."""
        new = 0
        for src in self.sources:
            for line, ref in self._read_new_lines(src):
                ev = self.adapter.to_event(line, ref)
                if ev is not None:
                    self.corpus.add(ev)
                    new += 1
        if new:
            self._write_board()
        return new

    def _write_board(self) -> None:
        ts = datetime.now(timezone.utc).isoformat(timespec="seconds")
        header = (f"# Atlas — board de melhor caminho\n"
                  f"# atualizado {ts} · {len(self.corpus)} evento(s) · advisory, [UNTESTED]\n\n")
        self.board_path.parent.mkdir(parents=True, exist_ok=True)
        self.board_path.write_text(header + self.engine.render_board(self.corpus),
                                   encoding="utf-8")

    def run(self, interval: float = 2.0, max_polls: int | None = None) -> None:
        polls = 0
        while max_polls is None or polls < max_polls:
            self.poll_once()
            polls += 1
            time.sleep(interval)
