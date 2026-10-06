"""Identidade fixada no pacote de release; o checkout se identifica como dev."""

try:
    from ._build import REVISION, VERSION
except ModuleNotFoundError:
    VERSION = "dev"
    REVISION = "unknown"


def report() -> dict[str, str]:
    return {"name": "hive-atlas", "version": VERSION, "revision": REVISION}
