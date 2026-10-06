#!/usr/bin/env python3
"""Monta o cliente stdlib em um zipapp portátil, sem dependências de build."""

from __future__ import annotations

import argparse
import re
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ENTRYPOINT = b'''import sys
if sys.version_info < (3, 10):
    print("hive atlas: Python 3.10 ou superior e obrigatorio", file=sys.stderr)
    raise SystemExit(2)
from atlas.__main__ import main
raise SystemExit(main())
'''


def build(version: str, revision: str, output_dir: Path) -> Path:
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?", version):
        raise ValueError("versão inválida: use vX.Y.Z")
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("revisão inválida: informe o SHA completo do commit")
    entries = {f"atlas/{p.name}": p.read_bytes() for p in (ROOT / "atlas").glob("*.py")}
    entries["__main__.py"] = ENTRYPOINT
    entries["atlas/_build.py"] = f"VERSION = {version!r}\nREVISION = {revision!r}\n".encode()
    entries["atlas/data/core.json"] = (ROOT / "signals/core.json").read_bytes()
    output_dir.mkdir(parents=True, exist_ok=True)
    target = output_dir / f"atlas-{version}.pyz"
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted(entries.items()):
            # Metadados fixos: a mesma fonte e identidade produzem o mesmo pacote.
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            archive.writestr(info, data)
    return target


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--output-dir", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    try:
        print(build(args.version, args.revision, args.output_dir))
    except ValueError as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
