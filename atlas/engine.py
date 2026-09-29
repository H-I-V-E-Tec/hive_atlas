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

from .evidence import Corpus
from .ficha import AUSENTE, PRESENTE, Ficha

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
    def _eval_conditions(self, ficha: Ficha, local: list[str], context: list[str]):
        """Condições `refute` são avaliadas sobre a evidência LOCAL que disparou o
        sinal (é sobre o campo observado); condições `required` sobre o CONTEXTO do
        fluxo (é sobre o ambiente). Retorna também required_absent."""
        present, absent, unknown, refuted, required_absent = [], [], [], False, False
        for c in ficha.conditions:
            if c.role == "refute":
                if c.resolve(local) == PRESENTE:
                    refuted = True
                    absent.append(f"{c.means} (contraexemplo presente)")
                continue
            status = c.resolve(context)
            if status == PRESENTE:
                present.append(c.means)
            elif status == AUSENTE:
                absent.append(c.means)
                required_absent = True
            else:
                unknown.append(c.means)
        return present, absent, unknown, refuted, required_absent

    def _verdict(self, refuted: bool, absent: list[str], unknown: list[str],
                 required_absent: bool) -> str:
        if refuted or required_absent:
            return DESCARTADO
        if unknown:
            return CANDIDATO
        return FORTE

    def _expand_candidates(self, ids: set[str]) -> set[str]:
        """Inclui feeders de chains candidatas e chains alimentadas por sinais
        candidatos, para o pré-filtro não quebrar as composições."""
        out = set(ids)
        for fid in list(ids):
            f = self.library.get(fid)
            if not f:
                continue
            out.update(f.requires)   # chain → seus feeders
            out.update(f.feeds)      # sinal → chains que ele alimenta
        return out

    def select_candidates(self, corpus: Corpus, retriever, k: int = 5) -> set[str]:
        """Usa a recuperação para escolher fichas candidatas (orçamento de k)."""
        query = " ".join(corpus.haystacks())
        hits = {fid for fid, _ in retriever.search(query, k)}
        return self._expand_candidates(hits)

    def evaluate(self, corpus: Corpus, only: set[str] | None = None) -> dict[str, Recommendation]:
        haystacks = corpus.haystacks()
        results: dict[str, Recommendation] = {}

        _rank = {FORTE: 0, CANDIDATO: 1, DESCARTADO: 2}

        # Pass 1: sinais únicos — avaliados POR OCORRÊNCIA (evidência que dispara).
        for fid, ficha in self.library.items():
            if ficha.kind != "signal":
                continue
            if only is not None and fid not in only:
                continue
            occurrences = [ev for ev in corpus
                           if any(rx.search(ev.haystack()) for rx in ficha._pats)]
            if not occurrences:
                continue
            best: Recommendation | None = None
            for ev in occurrences:
                present, absent, unknown, refuted, required_absent = self._eval_conditions(
                    ficha, local=[ev.haystack()], context=haystacks)
                verdict = self._verdict(refuted, absent, unknown, required_absent)
                rec = Recommendation(
                    ficha=ficha, verdict=verdict,
                    observation=ev.haystack(), ref=ev.ref,
                    present=present, absent=absent, unknown=unknown,
                )
                if best is None or _rank[verdict] < _rank[best.verdict]:
                    best = rec
            results[fid] = best  # type: ignore[assignment]

        # Pass 2: composições (C-*). Só encadeiam sobre feeders que dispararam
        # e não foram descartados.
        for fid, ficha in self.library.items():
            if ficha.kind != "chain":
                continue
            if only is not None and fid not in only:
                continue
            feeders = [results.get(r) for r in ficha.requires]
            fired = [f for f in feeders if f and f.is_lead]
            if len(fired) != len(ficha.requires):
                # feeder faltando ou descartado → a chain não se sustenta
                continue
            present, absent, unknown, refuted, required_absent = self._eval_conditions(
                ficha, local=haystacks, context=haystacks)
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
    def recommend(self, corpus: Corpus, retriever=None, k: int = 5) -> list[Recommendation]:
        """Board rankeado: só leads (forte/candidato); chains antes de únicos,
        depois por peso de classe, forte antes de candidato.

        Se `retriever` for dado, pré-seleciona até k fichas candidatas (mais os
        feeders necessários) antes de avaliar condições — o orçamento por fluxo.

        Avalia POR FLUXO (scope_key) e agrega: uma condição refute de um fluxo não
        derruba um sinal de outro fluxo."""
        best: dict[str, Recommendation] = {}
        for _scope, sub in corpus.by_scope().items():
            only = self.select_candidates(sub, retriever, k) if retriever is not None else None
            for fid, rec in self.evaluate(sub, only=only).items():
                if not rec.is_lead:
                    continue
                prev = best.get(fid)
                # mantém o melhor veredito por ficha entre fluxos (forte > candidato)
                if prev is None or (prev.verdict != FORTE and rec.verdict == FORTE):
                    best[fid] = rec
        leads = list(best.values())
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
