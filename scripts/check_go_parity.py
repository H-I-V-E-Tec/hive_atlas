#!/usr/bin/env python3
"""Compare actual stdio Go tool responses with the Python reference engine."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))
from atlas.engine import Engine
from atlas.evidence import Corpus
from atlas.ficha import load_library
from atlas.retrieval import LexicalRetriever


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    args = parser.parse_args()
    engine = Engine(load_library(ROOT / "signals/core.json"))
    cases = json.loads((ROOT / "eval/cases.json").read_text())["cases"]
    requests = [{"jsonrpc": "2.0", "id": 0, "method": "initialize", "params": {"protocolVersion": "2025-11-25"}},
                {"jsonrpc": "2.0", "method": "notifications/initialized"}]
    comparisons = []
    for case in cases:
        for k in (0, 5):
            requests.append({"jsonrpc": "2.0", "id": len(comparisons)+1, "method": "tools/call", "params": {"name": "atlas_observe", "arguments": {"evidence": case["evidence"], "k": k}}})
            comparisons.append((case, k))
    run = subprocess.run([str(Path(args.binary).resolve()), "--offline"], input="".join(json.dumps(r)+"\n" for r in requests), capture_output=True, text=True, cwd=ROOT, env={**os.environ, "ATLAS_REQUIRE_LICENSE": "0", "ATLAS_LIBRARY": str(ROOT / "signals/core.json")}, check=True)
    responses = [json.loads(line) for line in run.stdout.splitlines()]
    assert len(responses) == len(comparisons)+1
    for response, (case, k) in zip(responses[1:], comparisons):
        assert response["result"]["isError"] is False, response
        actual = json.loads(response["result"]["content"][0]["text"])["leads"]
        expected = engine.recommend(Corpus.from_dicts(case["evidence"]), retriever=LexicalRetriever(engine.library) if k else None, k=k or 5)
        fields = ("verdict", "present", "absent", "unknown", "version")
        go = {r["id"]: {f: r[f] for f in fields} for r in actual}
        py = {r.ficha.id: {"verdict": r.verdict, "present": r.present, "absent": r.absent, "unknown": r.unknown, "version": r.ficha.version} for r in expected}
        assert go == py, (case["id"], k, go, py)
        assert all(r["state"] == "UNTESTED" for r in actual)
    print(f"Go/Python parity: {len(cases)} reviewed cases, full and budgeted retrieval")


if __name__ == "__main__":
    main()
