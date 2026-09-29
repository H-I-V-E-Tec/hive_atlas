"""engine.py — motor incremental de recomendação.

Cruza a biblioteca de fichas contra a evidência da sessão e emite um board de
"melhor caminho": sinais candidatos rankeados, sempre `[UNTESTED]`. O mesmo motor
roda sobre um snapshot (batch) ou realimentado ao vivo por um watcher.

Verdicts de um sinal:
  forte      — dispara e todas as condições required estão `presente`.
  candidato  — dispara, nenhuma required `ausente`, mas alguma `desconhecida`.
  descartado — condição `refute` `presente`, ou alguma required `ausente`.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from .evidence import Corpus, Evidence
from .ficha import (
    AUSENTE, DESCONHECIDA, PRESENTE, Ficha,
)

FORTE = "forte"
CANDIDATO = "candidato"
DESCARTADO = "descartado"


@dataclass
class Recommendation:
    ficha: Ficha
    verdict: str
    observation: str          # observação concreta
    ref: str                  # referência da evidência
    present: list[str] = field(default_factory=list)
    absent: list[str] = field(default_factory=list)
    unknown: list[str] = field(default_factory=list)

    @property
    def is_lead(self) -> bool:
        return self.verdict in (FORTE, CANDIDATO)

    def render(self) -> str:
        f = self.ficha
        lines = [
            f"{f.id} v{f.version}  [{self.verdict}]",
            f"  observação: {self.observation}  (ref: {self.ref or '—'})",
            f"  por quê: {f.mechanism or '—'}",
            f"  pré-condições: presentes={self.present or '—'} "
            f"ausentes={self.absent or '—'} desconhecidas={self.unknown or '—'}",
            f"  contraexemplo: {f.counterexample or '—'}",
            f"  confirmação sugerida: {f.confirmation or '—'}",
            "  estado: hipótese ainda não testada  [UNTESTED]",
        ]
        return "\n".join(lines)


class Engine:
    def __init__(self, library: dict[str, Ficha]):
        self.library = library

    # ---- avaliação ----
    def _first_match(self, ficha: Ficha, corpus: Corpus) -> Evidence | None:
        for ev in corpus:
            if any(rx.search(ev.haystack()) for rx in ficha._pats):
                return ev
        return None

    def _eval_conditions(self, ficha: Ficha, haystacks: list[str]):
        present, absent, unknown, refuted = [], [], [], False
        for c in ficha.conditions:
            status = c.resolve(haystacks)
            if c.role == "refute":
                if status == PRESENTE:
                    refuted = True
                    absent.append(f"{c.means} (contraexemplo presente)")
                continue
            if status == PRESENTE:
                present.append(c.means)
            elif status == AUSENTE:
                absent.append(c.means)
            else:
                unknown.append(c.means)
        return present, absent, unknown, refuted

    def _verdict(self, refuted: bool, absent: list[str], unknown: list[str],
                 required_absent: bool) -> str:
        if refuted or required_absent:
            return DESCARTADO
        if unknown:
            return CANDIDATO
        return FORTE

    def evaluate(self, corpus: Corpus) -> dict[str, Recommendation]:
        haystacks = corpus.haystacks()
        results: dict[str, Recommendation] = {}

        # Pass 1: sinais únicos
        for fid, ficha in self.library.items():
            if ficha.kind != "signal":
                continue
            if not ficha.triggers(haystacks):
                continue
            present, absent, unknown, refuted = self._eval_conditions(ficha, haystacks)
            required_absent = any(
                c.role == "required" and c.resolve(haystacks) == AUSENTE
                for c in ficha.conditions
            )
            verdict = self._verdict(refuted, absent, unknown, required_absent)
            ev = self._first_match(ficha, corpus)
            results[fid] = Recommendation(
                ficha=ficha, verdict=verdict,
                observation=(ev.haystack() if ev else ""),
                ref=(ev.ref if ev else ""),
                present=present, absent=absent, unknown=unknown,
            )

        # Pass 2: composições (C-*). Só encadeiam sobre feeders que dispararam
        # e não foram descartados.
        for fid, ficha in self.library.items():
            if ficha.kind != "chain":
                continue
            feeders = [results.get(r) for r in ficha.requires]
            fired = [f for f in feeders if f and f.is_lead]
            if len(fired) != len(ficha.requires):
                # feeder faltando ou descartado → a chain não se sustenta
                continue
            present, absent, unknown, refuted = self._eval_conditions(ficha, haystacks)
            required_absent = any(
                c.role == "required" and c.resolve(haystacks) == AUSENTE
                for c in ficha.conditions
            )
            verdict = self._verdict(refuted, absent, unknown, required_absent)
            if verdict == DESCARTADO:
                continue
            # herda incerteza dos feeders
            if any(f.verdict == CANDIDATO for f in fired) and verdict == FORTE:
                verdict = CANDIDATO
            anchor = fired[0]
            results[fid] = Recommendation(
                ficha=ficha, verdict=verdict,
                observation=f"encadeia {', '.join(ficha.requires)}: {anchor.observation}",
                ref=anchor.ref,
                present=present + [f"feeder {r} disparou" for r in ficha.requires],
                absent=absent, unknown=unknown,
            )
        return results

    # ---- board ----
    def recommend(self, corpus: Corpus) -> list[Recommendation]:
        """Board rankeado: só leads (forte/candidato); chains antes de únicos,
        depois por peso de classe, forte antes de candidato."""
        results = self.evaluate(corpus)
        leads = [r for r in results.values() if r.is_lead]
        leads.sort(key=lambda r: (
            0 if r.ficha.kind == "chain" else 1,
            r.ficha.weight,
            0 if r.verdict == FORTE else 1,
            r.ficha.id,
        ))
        return leads

    def render_board(self, corpus: Corpus) -> str:
        leads = self.recommend(corpus)
        if not leads:
            return "sem leads: nenhum sinal com pré-condições sustentadas.  [UNTESTED]"
        return "\n\n".join(r.render() for r in leads)
