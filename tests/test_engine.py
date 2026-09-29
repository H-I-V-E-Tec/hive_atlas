"""Testes do núcleo do Atlas.

Foco: o motor mata o falso positivo que o hypotheses.py (regex puro) não mata,
verificando CONDIÇÕES. Caso de regressão herdado do PLANO-DOJO:
paginação com campo `next` NÃO deve disparar S-REDIR-01 nem C-02.
"""

from pathlib import Path

import pytest

from atlas import Corpus, Engine, load_library
from atlas.engine import CANDIDATO, DESCARTADO, FORTE

LIB = Path(__file__).resolve().parent.parent / "signals" / "core.json"


@pytest.fixture
def engine() -> Engine:
    return Engine(load_library(LIB))


def test_library_loads(engine: Engine):
    assert {"S-IDOR-01", "S-REDIR-01", "C-02"} <= set(engine.library)


def test_regressao_next_paginacao_nao_dispara_redirect_nem_chain(engine: Engine):
    """O caso sintético do PLANO-DOJO: paginação de lista pública com `next`."""
    corpus = Corpus.from_dicts([
        {
            "kind": "nota",
            "program_id": "acme",
            "asset": "api.acme.com",
            "flow": "listagem",
            "endpoint": "/v1/items",
            "method": "GET",
            "value": "GET /v1/items?next=<cursor> — paginação de lista pública",
            "ref": "targets/acme/recon.txt:10",
        }
    ])
    results = engine.evaluate(corpus)

    # S-REDIR-01 acende no pattern (`next`) mas é DESCARTADO pela condição refute.
    assert results["S-REDIR-01"].verdict == DESCARTADO
    # C-02 não pode encadear sobre um feeder descartado.
    assert "C-02" not in results

    board = engine.recommend(corpus)
    assert all(r.ficha.id != "C-02" for r in board)
    assert all(r.ficha.id != "S-REDIR-01" for r in board)


def test_oauth_real_dispara_chain_c02(engine: Engine):
    """Redirect controlável num fluxo OAuth real → C-02 vira lead forte."""
    corpus = Corpus.from_dicts([
        {
            "kind": "js_finding",
            "program_id": "acme",
            "asset": "auth.acme.com",
            "flow": "oauth",
            "endpoint": "/authorize",
            "value": "GET /authorize?client_id=abc&redirect_uri=https://app/cb&response_type=code",
            "ref": "targets/acme/js/main.js:88",
        }
    ])
    results = engine.evaluate(corpus)
    assert results["S-REDIR-01"].verdict == FORTE
    assert results["C-02"].verdict == FORTE

    board = engine.recommend(corpus)
    # chain vem antes de sinal único no ranking
    assert board[0].ficha.id == "C-02"


def test_redirect_sem_oauth_nao_encadeia(engine: Engine):
    """redirect_uri refletido mas sem fluxo OAuth: S-REDIR-01 forte, C-02 não fira."""
    corpus = Corpus.from_dicts([
        {
            "kind": "param",
            "program_id": "acme",
            "asset": "www.acme.com",
            "flow": "nav",
            "value": "?returnTo=https://evil.example refletido no Location",
            "ref": "brain/wal#7",
        }
    ])
    results = engine.evaluate(corpus)
    assert results["S-REDIR-01"].verdict == FORTE
    # oauth_presente é required e AUSENTE → C-02 descartado (não entra no board)
    assert "C-02" not in results


def test_board_marca_untested(engine: Engine):
    corpus = Corpus.from_dicts([
        {"kind": "param", "value": "userId=123 no path que retorna dado", "ref": "x"}
    ])
    text = engine.render_board(corpus)
    assert "[UNTESTED]" in text
