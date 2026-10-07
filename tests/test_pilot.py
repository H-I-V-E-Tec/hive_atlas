import json

import pytest

from atlas.mcp_server import Server, PROTOCOL_VERSION, SUPPORTED_PROTOCOL_VERSIONS
from atlas.watcher import LineAdapter
from atlas.sanitize import has_secret


@pytest.mark.parametrize("version", [*SUPPORTED_PROTOCOL_VERSIONS, "unsupported"])
def test_protocol_negotiation(version):
    response = Server().handle({"jsonrpc": "2.0", "id": 0, "method": "initialize",
                                "params": {"protocolVersion": version}})
    assert response["result"]["protocolVersion"] == (version if version in SUPPORTED_PROTOCOL_VERSIONS else PROTOCOL_VERSION)


def test_ping_and_notification():
    server = Server()
    assert server.handle({"jsonrpc": "2.0", "id": 0, "method": "ping"})["result"] == {}
    assert server.handle({"jsonrpc": "2.0", "method": "notifications/initialized"}) is None


def test_burp_program_boundary_and_secrets():
    adapter = LineAdapter(program_id="demo")
    event = {"schema_version": 1, "event_id": "synthetic", "program_id": "demo", "kind": "param", "value": "redirect_uri callback", "flow": "oauth"}
    assert adapter.to_event(json.dumps(event), "events.jsonl:1").program_id == "demo"
    event["program_id"] = "other"
    assert adapter.to_event(json.dumps(event), "events.jsonl:2") is None
    assert adapter.to_event('{"kind":"nota","value":"Authorization: Bearer synthetic"}', "events.jsonl:3") is None
    assert adapter.to_event("Cookie: session=synthetic", "events.jsonl:4") is None
    assert adapter.to_event("{broken}", "events.jsonl:5") is None
    assert (adapter.rejected_program, adapter.rejected_secret, adapter.rejected_invalid) == (1, 2, 1)


@pytest.mark.parametrize("line", [
    r'{"kind":"nota","value":"Authorization:\tBearer synthetic"}',
    r'{"kind":"nota","Authoriz\u0061tion":"Bearer synthetic"}',
])
def test_escaped_json_credentials_are_refused(line):
    assert has_secret(line)
    assert LineAdapter(program_id="demo").to_event(line, "fixture:1") is None
