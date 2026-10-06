"""Exercita o pacote real fora do checkout, sem Qdrant, Ollama ou licença."""

import json
import os
import subprocess
import sys
import zipfile

import pytest

from scripts.build_client_release import build

VERSION = "v0.2.0"
REVISION = "a" * 40


@pytest.fixture
def package(tmp_path):
    return build(VERSION, REVISION, tmp_path / "dist")


def run_package(package, tmp_path, args=(), requests=(), **overrides):
    env = os.environ.copy()
    for key in ("QDRANT_API_KEY", "ATLAS_REQUIRE_LICENSE", "ATLAS_LICENSE", "ATLAS_LICENSE_KEY", "ATLAS_FEEDBACK_FILE"):
        env.pop(key, None)
    env["HIVE_HOME"] = str(tmp_path / "hive")
    env.update(overrides)
    return subprocess.run(
        [sys.executable, "-I", str(package), *args], cwd=tmp_path, env=env,
        input="".join(json.dumps(req) + "\n" for req in requests),
        encoding="utf-8", capture_output=True, timeout=15,
    )


def test_package_version_without_license_or_services(package, tmp_path):
    proc = run_package(package, tmp_path, args=("version", "--json"), ATLAS_REQUIRE_LICENSE="1")
    assert proc.returncode == 0, proc.stderr
    assert json.loads(proc.stdout) == {"name": "hive-atlas", "version": VERSION, "revision": REVISION}
    assert proc.stderr == ""


def test_package_serves_bundled_library_and_records_feedback(package, tmp_path):
    proc = run_package(package, tmp_path, requests=(
        {"jsonrpc": "2.0", "id": 1, "method": "initialize"},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
        {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {
            "name": "atlas_signals_search", "arguments": {"query": "oauth redirect_uri client_id", "k": 3}}},
        {"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": {
            "name": "atlas_feedback", "arguments": {"signal_id": "C-02", "outcome": "confirmado"}}},
    ))
    assert proc.returncode == 0, proc.stderr
    responses = [json.loads(line) for line in proc.stdout.splitlines()]
    assert [r["id"] for r in responses] == [1, 2, 3, 4]
    assert responses[0]["result"]["serverInfo"]["version"] == VERSION
    assert len(responses[1]["result"]["tools"]) == 3
    hits = json.loads(responses[2]["result"]["content"][0]["text"])
    assert any(hit["id"] == "C-02" for hit in hits)
    assert responses[3]["result"]["isError"] is False
    feedback = tmp_path / "hive/atlas/feedback.jsonl"
    assert json.loads(feedback.read_text())["signal_id"] == "C-02"
    with zipfile.ZipFile(package) as archive:
        assert not any("feedback" in name and name.endswith(".jsonl") for name in archive.namelist())


def test_package_license_gate_remains_fail_closed(package, tmp_path):
    proc = run_package(package, tmp_path, ATLAS_REQUIRE_LICENSE="1")
    assert proc.returncode == 2
    assert proc.stdout == ""
    assert "licen" in proc.stderr.lower()


def test_package_reproducible_without_local_artifacts(package, tmp_path):
    other = build(VERSION, REVISION, tmp_path / "other")
    assert package.read_bytes() == other.read_bytes()
    with zipfile.ZipFile(package) as archive:
        names = archive.namelist()
        assert "atlas/data/core.json" in names
        assert "atlas/_build.py" in names
        assert not any("__pycache__" in name or name.startswith(("tests/", "deploy/")) for name in names)


@pytest.mark.parametrize("version,revision", [("master", REVISION), (VERSION, "abc"), ("v1.0.0/../bad", REVISION)])
def test_build_refuses_invalid_release_identity(tmp_path, version, revision):
    with pytest.raises(ValueError):
        build(version, revision, tmp_path)
