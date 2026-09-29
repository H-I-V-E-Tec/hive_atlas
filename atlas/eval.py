"""eval.py — harness de medição (Etapa 3 do plano).

Roda o Atlas (candidato) e o hypotheses.py (referência) sobre o MESMO conjunto de
casos revisados e reporta métricas por mecanismo. O ganho é uma conclusão medida
neste conjunto, não garantia para qualquer caçada futura.

Métricas por engine:
  recall (cobertura)         = TP / (TP + FN)
  precision (aplicabilidade) = TP / (TP + FP)
  regressões                 = leads que caíram em `forbidden`

Uso:
  python3 -m atlas.eval                 # tabela comparativa
  python3 -m atlas.eval --json
"""

from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass, field
from pathlib import Path

from .evidence import Corpus
from .engine import Engine
from .ficha import load_library

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_CASES = ROOT / "eval" / "cases.json"
DEFAULT_LIB = ROOT / "signals" / "core.json"


@dataclass
class Score:
    tp: int = 0
    fp: int = 0
    fn: int = 0
    regressions: int = 0
    regression_cases: list[str] = field(default_factory=list)

    def add(self, expected: set[str], forbidden: set[str], leads: set[str], case_id: str):
        self.tp += len(expected & leads)
        self.fp += len(leads - expected)
        self.fn += len(expected - leads)
        hit = leads & forbidden
        if hit:
            self.regressions += len(hit)
            self.regression_cases.append(case_id)

    @property
    def recall(self) -> float:
        d = self.tp + self.fn
        return self.tp / d if d else 1.0

    @property
    def precision(self) -> float:
        d = self.tp + self.fp
        return self.tp / d if d else 1.0

    def as_dict(self) -> dict:
        return {
            "tp": self.tp, "fp": self.fp, "fn": self.fn,
            "recall": round(self.recall, 3), "precision": round(self.precision, 3),
            "regressions": self.regressions, "regression_cases": self.regression_cases,
        }


def _load_cases(path: Path) -> list[dict]:
    return json.loads(path.read_text(encoding="utf-8"))["cases"]


def run_atlas(cases: list[dict], lib_path: Path) -> tuple[dict, dict]:
    """Retorna (score_total, scores_por_mecanismo) do Atlas."""
    eng = Engine(load_library(lib_path))
    total = Score()
    by_mech: dict[str, Score] = {}
    for c in cases:
        corpus = Corpus.from_dicts(c["evidence"])
        leads = {r.ficha.id for r in eng.recommend(corpus)}
        exp, forb = set(c["expected"]), set(c["forbidden"])
        total.add(exp, forb, leads, c["id"])
        by_mech.setdefault(c["mechanism"], Score()).add(exp, forb, leads, c["id"])
    return total.as_dict(), {k: v.as_dict() for k, v in by_mech.items()}


def _baseline_dir() -> Path | None:
    env = os.environ.get("ATLAS_EXCALIBULL_DIR")
    if env:
        return Path(env)
    guess = ROOT.parent.parent / "excalibull"   # HIVE/.. /excalibull
    return guess if (guess / "tools" / "hypotheses.py").exists() else None


def run_baseline(cases: list[dict]) -> tuple[dict, dict] | None:
    """Roda o hypotheses.py chamando parse_signals/evaluate direto (o CLI tem um
    bug no default de --program). Retorna None se o baseline não estiver
    disponível."""
    base = _baseline_dir()
    if not base:
        return None
    tools = base / "tools"
    signals = base / "memory" / "signals.md"
    sys.path.insert(0, str(tools))
    _saved_argv = sys.argv
    sys.argv = ["hypotheses"]
    try:
        import hypotheses as h  # type: ignore
        entries = h.parse_signals(signals.read_text(encoding="utf-8"))
    except Exception:
        return None
    finally:
        sys.argv = _saved_argv

    total = Score()
    by_mech: dict[str, Score] = {}
    for c in cases:
        leads = {x.get("id") for x in h.evaluate(entries, c["text"])}
        exp, forb = set(c["expected"]), set(c["forbidden"])
        total.add(exp, forb, leads, c["id"])
        by_mech.setdefault(c["mechanism"], Score()).add(exp, forb, leads, c["id"])
    return total.as_dict(), {k: v.as_dict() for k, v in by_mech.items()}


def report(cases_path: Path = DEFAULT_CASES, lib_path: Path = DEFAULT_LIB) -> dict:
    cases = _load_cases(cases_path)
    atlas_total, atlas_mech = run_atlas(cases, lib_path)
    baseline = run_baseline(cases)
    out = {
        "n_cases": len(cases),
        "atlas": {"total": atlas_total, "por_mecanismo": atlas_mech},
    }
    if baseline:
        b_total, b_mech = baseline
        out["baseline"] = {"total": b_total, "por_mecanismo": b_mech}
    else:
        out["baseline"] = None
    return out


def _fmt(name: str, s: dict) -> str:
    return (f"  {name:10s} recall={s['recall']:.2f} precision={s['precision']:.2f} "
            f"TP={s['tp']} FP={s['fp']} FN={s['fn']} regressões={s['regressions']} "
            f"{s['regression_cases'] or ''}")


def main(argv: list[str] | None = None) -> int:
    argv = argv if argv is not None else sys.argv[1:]
    data = report()
    if "--json" in argv:
        print(json.dumps(data, ensure_ascii=False, indent=2))
        return 0
    print(f"Casos revisados: {data['n_cases']}\n")
    print("TOTAL")
    print(_fmt("atlas", data["atlas"]["total"]))
    if data["baseline"]:
        print(_fmt("baseline", data["baseline"]["total"]))
    else:
        print("  baseline   indisponível (defina ATLAS_EXCALIBULL_DIR)")
    print("\nPor mecanismo (Atlas):")
    for mech, s in sorted(data["atlas"]["por_mecanismo"].items()):
        print(_fmt(mech, s))
    if data["baseline"]:
        print("\nPor mecanismo (baseline):")
        for mech, s in sorted(data["baseline"]["por_mecanismo"].items()):
            print(_fmt(mech, s))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
