"""Ingestão fail-closed (Etapa 7)."""

import json
import shutil
from pathlib import Path

import pytest

from atlas.ingest import IngestError, ingest

ROOT = Path(__file__).resolve().parent.parent


@pytest.fixture
def lib_copy(tmp_path: Path) -> Path:
    dst = tmp_path / "core.json"
    shutil.copy(ROOT / "signals" / "core.json", dst)
    return dst


def _manifest(tmp_path: Path, signals: list[dict]) -> Path:
    p = tmp_path / "manifest.json"
    p.write_text(json.dumps({"schema_version": 1, "produced_by": "dojo", "signals": signals}),
                 encoding="utf-8")
    return p


def _cases() -> Path:
    return ROOT / "eval" / "cases.json"


def test_aceita_ficha_nova_valida(tmp_path, lib_copy):
    m = _manifest(tmp_path, [{
        "id": "S-BAC-01", "version": 1, "state": "revisada", "kind": "signal",
        "signal_class": "BAC",
        "mechanism": "controle casa só uma forma do request; variante passa.",
        "patterns": ["\\b(401|403)\\b"],
        "counterexample": "rota que barra em todas as variantes.",
    }])
    summary = ingest(m, lib_copy, _cases())
    assert summary["ingeridas"] == 1
    ids = {f["id"] for f in json.loads(lib_copy.read_text())["signals"]}
    assert "S-BAC-01" in ids


def test_rejeita_estado_candidata(tmp_path, lib_copy):
    m = _manifest(tmp_path, [{
        "id": "S-X-01", "version": 1, "state": "candidata", "kind": "signal",
        "mechanism": "x", "patterns": ["x"],
    }])
    with pytest.raises(IngestError) as e:
        ingest(m, lib_copy, _cases())
    assert any("estado" in r for r in e.value.reasons)


def test_rejeita_vazamento_de_programa(tmp_path, lib_copy):
    m = _manifest(tmp_path, [{
        "id": "S-Y-01", "version": 1, "state": "revisada", "kind": "signal",
        "mechanism": "falha vista em https://api.cliente-secreto.com/v1",
        "patterns": ["token"],
    }])
    with pytest.raises(IngestError) as e:
        ingest(m, lib_copy, _cases())
    assert any("vazamento" in r for r in e.value.reasons)


def test_rejeita_duplicata_sem_bump_de_versao(tmp_path, lib_copy):
    m = _manifest(tmp_path, [{
        "id": "S-REDIR-01", "version": 1, "state": "revisada", "kind": "signal",
        "mechanism": "redirect controlável", "patterns": ["redirect"],
    }])
    with pytest.raises(IngestError) as e:
        ingest(m, lib_copy, _cases())
    assert any("version deve aumentar" in r for r in e.value.reasons)


def test_falha_fechado_nao_escreve(tmp_path, lib_copy):
    before = lib_copy.read_text()
    m = _manifest(tmp_path, [{
        "id": "S-Z-01", "version": 1, "state": "candidata", "kind": "signal",
        "mechanism": "x", "patterns": ["x"],
    }])
    with pytest.raises(IngestError):
        ingest(m, lib_copy, _cases())
    assert lib_copy.read_text() == before  # nada mudou
