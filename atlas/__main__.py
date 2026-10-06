"""Entrada do cliente: sem argumentos inicia o MCP; version identifica o pacote."""

import argparse
import json

from .version import report


def main() -> int:
    parser = argparse.ArgumentParser(prog="hive atlas")
    parser.add_argument("command", nargs="?", choices=("mcp", "version"))
    parser.add_argument("--json", action="store_true", help="versão em JSON")
    args = parser.parse_args()
    if args.command == "version":
        # JSON também sem --json: contrato do teste de instalação do launcher.
        print(json.dumps(report()))
        return 0
    if args.json:
        parser.error("--json exige o comando version")
    from .mcp_server import main as serve

    return serve()


if __name__ == "__main__":
    raise SystemExit(main())
