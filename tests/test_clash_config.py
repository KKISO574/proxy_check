from __future__ import annotations

import yaml

from app.core.clash_config import build_runtime_config, load_clash_nodes


def test_load_clash_nodes_deduplicates_static_proxies(tmp_path):
    config = tmp_path / "config.yaml"
    config.write_text(
        yaml.safe_dump(
            {
                "proxies": [
                    {"name": "node-a", "type": "ss", "server": "a.example.com", "port": 443},
                    {"name": "node-a", "type": "ss", "server": "duplicate.example.com", "port": 443},
                    {"name": "node-b", "type": "vmess", "server": "b.example.com", "port": 8443},
                    {"type": "ss", "server": "missing-name.example.com"},
                ]
            }
        ),
        encoding="utf-8",
    )

    nodes = load_clash_nodes(config)

    assert [node.name for node in nodes] == ["node-a", "node-b"]
    assert nodes[0].server == "a.example.com"
    assert nodes[1].type == "vmess"


def test_build_runtime_config_injects_controller_and_listeners(tmp_path):
    source = tmp_path / "source.yaml"
    output = tmp_path / "runtime" / "config.yaml"
    source.write_text(
        yaml.safe_dump(
            {
                "mode": "rule",
                "proxies": [
                    {"name": "node-a", "type": "ss", "server": "a.example.com", "port": 443},
                    {"name": "node-b", "type": "ss", "server": "b.example.com", "port": 443},
                ],
            }
        ),
        encoding="utf-8",
    )

    ports = build_runtime_config(
        source,
        output,
        controller_host="127.0.0.1",
        controller_port=9090,
        secret="secret",
        listener_host="127.0.0.1",
        listener_start_port=20000,
        node_names=["node-a", "node-b"],
    )

    data = yaml.safe_load(output.read_text(encoding="utf-8"))
    assert ports == {"node-a": 20000, "node-b": 20001}
    assert data["external-controller"] == "127.0.0.1:9090"
    assert data["secret"] == "secret"
    assert data["listeners"][0]["type"] == "mixed"
    assert data["listeners"][0]["proxy"] == "node-a"
    assert data["listeners"][1]["port"] == 20001

