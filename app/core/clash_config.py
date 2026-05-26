from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel


class ClashNode(BaseModel):
    name: str
    type: str | None = None
    server: str | None = None
    port: int | None = None
    raw: dict[str, Any]


def load_clash_nodes(config_path: str | Path) -> list[ClashNode]:
    path = Path(config_path)
    if not path.exists():
        raise FileNotFoundError(f"Clash config not found: {path}")

    with path.open("r", encoding="utf-8") as fh:
        data = yaml.safe_load(fh) or {}

    proxies = data.get("proxies")
    if not isinstance(proxies, list):
        return []

    nodes: list[ClashNode] = []
    seen: set[str] = set()
    for item in proxies:
        if not isinstance(item, dict):
            continue
        name = item.get("name")
        if not isinstance(name, str) or not name.strip() or name in seen:
            continue
        seen.add(name)
        port = item.get("port")
        nodes.append(
            ClashNode(
                name=name,
                type=item.get("type") if isinstance(item.get("type"), str) else None,
                server=item.get("server") if isinstance(item.get("server"), str) else None,
                port=port if isinstance(port, int) else None,
                raw=item,
            )
        )
    return nodes


def build_runtime_config(
    source_config_path: str | Path,
    output_path: str | Path,
    *,
    controller_host: str,
    controller_port: int,
    secret: str,
    listener_host: str,
    listener_start_port: int,
    node_names: list[str],
) -> dict[str, int]:
    source = Path(source_config_path)
    with source.open("r", encoding="utf-8") as fh:
        data = yaml.safe_load(fh) or {}

    data["external-controller"] = f"{controller_host}:{controller_port}"
    data["secret"] = secret

    listeners = list(data.get("listeners") or [])
    port_map: dict[str, int] = {}
    for index, name in enumerate(node_names):
        port = listener_start_port + index
        port_map[name] = port
        listeners.append(
            {
                "name": f"proxy-check-{index}",
                "type": "mixed",
                "listen": listener_host,
                "port": port,
                "proxy": name,
            }
        )

    data["listeners"] = listeners
    out = Path(output_path)
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open("w", encoding="utf-8") as fh:
        yaml.safe_dump(data, fh, allow_unicode=True, sort_keys=False)
    return port_map

