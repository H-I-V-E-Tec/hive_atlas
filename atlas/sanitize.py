"""Reject obvious credentials without logging their content.

The producer remains responsible for full credential and PII sanitization.
"""
import json
import re

_PATTERNS = [
    re.compile(r"authorization\s*[:=]\s*(bearer|basic)\s+\S+", re.I),
    re.compile(r"\b(cookie|set-cookie)\s*:\s*\S+", re.I),
    re.compile(r"\b(gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[A-Z0-9]{16}|sk-[A-Za-z0-9_-]{20,})\b"),
    re.compile(r'"(authorization|cookie|set-cookie|password|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)"\s*:\s*"[^"\s][^"]*"', re.I),
    re.compile(r'\b(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password)\s*[=:]\s*[^\s&"<>]+', re.I),
]


def has_secret(text: str) -> bool:
    text = text.replace('\\"', '"')
    if any(pattern.search(text) for pattern in _PATTERNS):
        return True
    try:
        decoded = json.loads(text)
    except (ValueError, RecursionError):
        return False

    pending = [decoded]
    while pending:
        value = pending.pop()
        if isinstance(value, str):
            if any(pattern.search(value) for pattern in _PATTERNS):
                return True
        elif isinstance(value, list):
            pending.extend(value)
        elif isinstance(value, dict):
            for key, item in value.items():
                if isinstance(item, str) and any(pattern.search(f"{key}: {item}") for pattern in _PATTERNS):
                    return True
                pending.append(item)
    return False
