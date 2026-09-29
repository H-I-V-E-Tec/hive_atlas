"""Sanidade do harness de avaliação (Etapa 3)."""

from atlas.eval import report


def test_atlas_sem_regressoes_e_precisao_perfeita_no_conjunto():
    data = report()
    atlas = data["atlas"]["total"]
    assert atlas["regressions"] == 0
    assert atlas["precision"] == 1.0
    assert atlas["recall"] == 1.0


def test_atlas_bate_o_baseline_quando_disponivel():
    """Se o excalibull estiver ao lado, o Atlas deve ter precisão >= baseline e
    menos regressões. Se indisponível, o teste passa (baseline None)."""
    data = report()
    base = data["baseline"]
    if base is None:
        return
    assert data["atlas"]["total"]["precision"] >= base["total"]["precision"]
    assert data["atlas"]["total"]["regressions"] <= base["total"]["regressions"]
