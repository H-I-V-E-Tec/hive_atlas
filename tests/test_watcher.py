"""Watcher incremental (Etapa 4): tail de arquivos → board ao vivo."""

from pathlib import Path

import pytest

from atlas import Engine, load_library
from atlas.watcher import LineAdapter, SessionWatcher

LIB = Path(__file__).resolve().parent.parent / "signals" / "core.json"


@pytest.fixture
def watcher(tmp_path: Path) -> SessionWatcher:
    src = tmp_path / "brain.wal"
    src.write_text("", encoding="utf-8")
    return SessionWatcher(
        engine=Engine(load_library(LIB)),
        sources=[src],
        board_path=tmp_path / "board.md",
        adapter=LineAdapter(program_id="acme", asset="auth.acme.com"),
    )


def _append(path: Path, line: str):
    with path.open("a", encoding="utf-8") as fh:
        fh.write(line + "\n")


def test_board_atualiza_incrementalmente_e_so_com_precondicao(watcher: SessionWatcher):
    src = watcher.sources[0]

    # 1) chega paginação `next` → nenhum lead (condição refute)
    _append(src, "GET /v1/items?next=<cursor> paginacao de lista publica")
    assert watcher.poll_once() == 1
    board = watcher.board_path.read_text(encoding="utf-8")
    assert "[UNTESTED]" in board
    assert "C-02" not in board

    # 2) chega o fluxo OAuth → agora C-02 vira lead
    _append(src, "GET /authorize?client_id=abc&redirect_uri=https://app/cb&response_type=code")
    assert watcher.poll_once() == 1
    board = watcher.board_path.read_text(encoding="utf-8")
    assert "C-02" in board
    assert "[UNTESTED]" in board


def test_poll_sem_novidade_nao_conta(watcher: SessionWatcher):
    assert watcher.poll_once() == 0


def test_linha_json_estruturada_e_parseada(watcher: SessionWatcher):
    src = watcher.sources[0]
    _append(src, '{"kind":"param","value":"userId=1 retorna dado","flow":"authz"}')
    watcher.poll_once()
    assert any(e.kind == "param" for e in watcher.corpus)
