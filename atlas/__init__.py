"""hive_atlas — biblioteca de sinais e motor de recomendação.

Núcleo dependency-light (stdlib). O motor raciocina sobre evidência JÁ coletada
numa sessão autorizada e emite sinais candidatos `[UNTESTED]`. Nunca toca o alvo,
nunca dispara teste, nunca autoriza tráfego.
"""

from .ficha import Ficha, Condition, load_library
from .evidence import Evidence, Corpus
from .engine import Engine, Recommendation

__all__ = [
    "Ficha",
    "Condition",
    "load_library",
    "Evidence",
    "Corpus",
    "Engine",
    "Recommendation",
]
