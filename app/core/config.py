from __future__ import annotations

import os
from functools import lru_cache
from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, Field


class AppConfig(BaseModel):
    database_url: str = "sqlite+aiosqlite:///./data/proxy_check.sqlite3"
    static_dir: str = "app/static"


class MihomoConfig(BaseModel):
    bin: str = ""
    source_config_path: str = ""
    work_dir: str = "./runtime/mihomo"
    imported_config_dir: str = "./runtime/configs"
    controller_host: str = "127.0.0.1"
    controller_port: int = 9090
    secret_env: str = "MIHOMO_SECRET"
    listener_host: str = "127.0.0.1"
    listener_port_start: int = 20000
    listener_port_max: int = 65000


class TcpTarget(BaseModel):
    host: str
    port: int

    @property
    def label(self) -> str:
        return f"{self.host}:{self.port}"


class ProbeConfig(BaseModel):
    interval_seconds: int = 60
    concurrency: int = 100
    timeout_ms: int = 5000
    import_timeout_ms: int = 30000
    retention_days: int = 30
    delay_url: str = "https://cp.cloudflare.com/generate_204"
    tcp_targets: list[TcpTarget] = Field(
        default_factory=lambda: [
            TcpTarget(host="1.1.1.1", port=443),
            TcpTarget(host="1.1.1.1", port=80),
            TcpTarget(host="8.8.8.8", port=443),
            TcpTarget(host="8.8.8.8", port=80),
        ]
    )


class Settings(BaseModel):
    app: AppConfig = Field(default_factory=AppConfig)
    mihomo: MihomoConfig = Field(default_factory=MihomoConfig)
    probe: ProbeConfig = Field(default_factory=ProbeConfig)


def _deep_merge(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    merged = dict(base)
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(merged.get(key), dict):
            merged[key] = _deep_merge(merged[key], value)
        else:
            merged[key] = value
    return merged


def load_settings(config_path: str | Path = "configs/config.yaml") -> Settings:
    path = Path(config_path)
    example_path = Path("configs/config.example.yaml")
    raw: dict[str, Any] = {}

    if example_path.exists():
        with example_path.open("r", encoding="utf-8") as fh:
            raw = yaml.safe_load(fh) or {}

    if path.exists():
        with path.open("r", encoding="utf-8") as fh:
            raw = _deep_merge(raw, yaml.safe_load(fh) or {})

    return Settings.model_validate(raw)


@lru_cache
def get_settings() -> Settings:
    return load_settings(os.environ.get("PROXY_CHECK_CONFIG", "configs/config.yaml"))
