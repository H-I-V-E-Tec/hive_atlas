"""ingest.py — ingestão de fichas do dojo (contrato dojo → Atlas, v1).

Ver docs/contrato-ingestao.md. Caminho de ESCRITA, separado do MCP de leitura.
Fail-closed: recusa o lote inteiro se qualquer ficha falhar um portão.

Portões:
  1. estado válido       — só `revisada`/`ativa` entram na biblioteca.
  2. schema completo      — Ficha.from_dict valida; id/version estáveis.
  3. confidencialidade    — campos operacionais program-agnostic (sanitização).
  4. enriquecer≠duplicar  — id existente exige version maior e merge de histórico.
  5. regressão            — após fusão de teste, casos `forbidden` continuam 0.
"""

from __future__ import annotations

import json
import re
import tempfile
from pathlib import Path

from .engine import Engine
from .eval import _load_cases, run_atlas
from .ficha import Ficha, SCHEMA_VERSION, load_library

ROOT = Path(__file__).resolve().parent.parent


class IngestError(Exception):
    def __init__(self, reasons: list[str]):
        self.reasons = reasons
        super().__init__("; ".join(reasons))


# marcadores de especificidade de programa que não podem vazar para a biblioteca
_LEAK = [
    (re.compile(r"https?://", re.I), "URL concreta"),
    (re.compile(r"\b\d{1,3}(?:\.\d{1,3}){3}\b"), "IP"),
    (re.compile(r"\b[\w.+-]+@[\w-]+\.[\w.-]+\b"), "e-mail"),
    (re.compile(r"\b[a-z0-9-]+\.(?:com|net|org|io|site|dev|app|co|gov|edu)\b", re.I), "domínio"),
]

# campos operacionais que precisam ser genéricos (origem/proveniência é revisada
# à mão pelo dojo e não é varrida aqui — ver docstring do contrato).
def _operational_text(f: dict) -> list[str]:
    parts = [f.get("mechanism", ""), f.get("counterexample", ""), f.get("confirmation", "")]
    parts += list(f.get("patterns", []))
    parts += [c.get("pattern", "") + " " + c.get("means", "") for c in f.get("conditions", [])]
    return [p for p in parts if p]


def _check_confidentiality(f: dict) -> list[str]:
    reasons = []
    for text in _operational_text(f):
        for rx, label in _LEAK:
            if rx.search(text):
                reasons.append(f"{f.get('id','?')}: possível vazamento ({label}) em campo operacional: {text!r}")
    return reasons


def _validate_ficha(f: dict, current: dict[str, dict]) -> list[str]:
    reasons: list[str] = []
    fid = f.get("id", "?")
    # 1. estado
    if f.get("state") not in ("revisada", "ativa"):
        reasons.append(f"{fid}: estado deve ser 'revisada' ou 'ativa' (é {f.get('state')!r})")
    # 2. schema
    try:
        Ficha.from_dict(f)
    except (TypeError, ValueError) as exc:
        reasons.append(f"{fid}: schema inválido: {exc}")
    if not f.get("mechanism"):
        reasons.append(f"{fid}: 'mechanism' obrigatório")
    if f.get("kind", "signal") == "chain" and not f.get("requires"):
        reasons.append(f"{fid}: chain sem 'requires'")
    # 3. confidencialidade
    reasons += _check_confidentiality(f)
    # 4. enriquecer ≠ duplicar
    if fid in current:
        if int(f.get("version", 1)) <= int(current[fid].get("version", 1)):
            reasons.append(f"{fid}: já existe; version deve aumentar (enriquecer, não duplicar)")
    return reasons


def _merge(current: list[dict], incoming: list[dict]) -> list[dict]:
    by_id = {f["id"]: dict(f) for f in current}
    for f in incoming:
        fid = f["id"]
        if fid in by_id:
            hist = by_id[fid].get("historico", [])
            hist.append(f"v{by_id[fid].get('version')}→v{f.get('version')}: atualizado por ingestão")
            f = dict(f)
            f["historico"] = hist
        by_id[fid] = f
    return list(by_id.values())


def _regression_ok(candidate_signals: list[dict], cases_path: Path) -> tuple[bool, dict]:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False, encoding="utf-8") as tmp:
        json.dump({"schema_version": SCHEMA_VERSION, "signals": candidate_signals}, tmp,
                  ensure_ascii=False)
        tmp_path = Path(tmp.name)
    try:
        total, _ = run_atlas(_load_cases(cases_path), tmp_path)
    finally:
        tmp_path.unlink(missing_ok=True)
    return total["regressions"] == 0, total


def ingest(manifest_path: str | Path,
           library_path: str | Path = ROOT / "signals" / "core.json",
           cases_path: str | Path = ROOT / "eval" / "cases.json",
           write: bool = True) -> dict:
    """Ingere um manifesto. Levanta IngestError (fail-closed) se algum portão
    falhar; nada é escrito nesse caso. Retorna um resumo em caso de sucesso."""
    manifest = json.loads(Path(manifest_path).read_text(encoding="utf-8"))
    if manifest.get("schema_version") != SCHEMA_VERSION:
        raise IngestError([f"schema_version incompatível: {manifest.get('schema_version')}"])
    incoming = manifest.get("signals", [])
    if not incoming:
        raise IngestError(["manifesto sem fichas"])

    lib_path = Path(library_path)
    current_raw = json.loads(lib_path.read_text(encoding="utf-8")).get("signals", [])
    current_by_id = {f["id"]: f for f in current_raw}

    # portões 1–4 (por ficha)
    reasons: list[str] = []
    for f in incoming:
        reasons += _validate_ficha(f, current_by_id)
    if reasons:
        raise IngestError(reasons)

    # portão 5 (regressão), sobre a fusão de teste
    candidate = _merge(current_raw, incoming)
    ok, total = _regression_ok(candidate, Path(cases_path))
    if not ok:
        raise IngestError([f"regressão: {total['regressions']} lead(s) proibido(s) "
                           f"em {total['regression_cases']}"])

    if write:
        out = {"schema_version": SCHEMA_VERSION,
               "produced_by": manifest.get("produced_by", "ingest"),
               "signals": candidate}
        tmp = lib_path.with_suffix(".json.tmp")
        tmp.write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
        tmp.replace(lib_path)  # troca atômica

    return {"ingeridas": len(incoming), "total_biblioteca": len(candidate),
            "regressions": total["regressions"]}
