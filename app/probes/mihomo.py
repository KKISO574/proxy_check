from __future__ import annotations

import asyncio
import os
from pathlib import Path
from urllib.parse import quote

import aiohttp

from app.core.clash_config import build_runtime_config, load_clash_nodes
from app.core.config import Settings


class MihomoUnavailable(RuntimeError):
    pass


class MihomoManager:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.process: asyncio.subprocess.Process | None = None
        self.runtime_config_path = Path(settings.mihomo.work_dir) / "config.yaml"
        self.listener_ports: dict[str, int] = {}

    @property
    def controller_base_url(self) -> str:
        cfg = self.settings.mihomo
        return f"http://{cfg.controller_host}:{cfg.controller_port}"

    @property
    def secret(self) -> str:
        return os.environ.get(self.settings.mihomo.secret_env, "")

    async def prepare(self) -> None:
        cfg = self.settings.mihomo
        if not cfg.source_config_path:
            raise MihomoUnavailable("mihomo.source_config_path is not configured")
        if not Path(cfg.source_config_path).exists():
            raise MihomoUnavailable(f"source config not found: {cfg.source_config_path}")
        if not self.secret:
            raise MihomoUnavailable(f"environment variable {cfg.secret_env} is not set")

        nodes = load_clash_nodes(cfg.source_config_path)
        self.listener_ports = build_runtime_config(
            cfg.source_config_path,
            self.runtime_config_path,
            controller_host=cfg.controller_host,
            controller_port=cfg.controller_port,
            secret=self.secret,
            listener_host=cfg.listener_host,
            listener_start_port=cfg.listener_port_start,
            node_names=[node.name for node in nodes],
        )

    async def start(self) -> None:
        cfg = self.settings.mihomo
        if not cfg.bin:
            raise MihomoUnavailable("mihomo.bin is not configured")
        if not Path(cfg.bin).exists():
            raise MihomoUnavailable(f"mihomo binary not found: {cfg.bin}")
        await self.prepare()
        if self.process and self.process.returncode is None:
            return
        self.process = await asyncio.create_subprocess_exec(
            cfg.bin,
            "-f",
            str(self.runtime_config_path),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )

    async def stop(self) -> None:
        if not self.process or self.process.returncode is not None:
            return
        self.process.terminate()
        try:
            await asyncio.wait_for(self.process.wait(), timeout=5)
        except TimeoutError:
            self.process.kill()
            await self.process.wait()


class MihomoClient:
    def __init__(self, base_url: str, secret: str, timeout_ms: int) -> None:
        self.base_url = base_url.rstrip("/")
        self.secret = secret
        self.timeout = aiohttp.ClientTimeout(total=timeout_ms / 1000)

    def _headers(self) -> dict[str, str]:
        if not self.secret:
            return {}
        return {"Authorization": f"Bearer {self.secret}"}

    async def delay(self, proxy_name: str, *, url: str, timeout_ms: int) -> float:
        endpoint = f"{self.base_url}/proxies/{quote(proxy_name, safe='')}/delay"
        params = {"url": url, "timeout": str(timeout_ms)}
        async with aiohttp.ClientSession(timeout=self.timeout, headers=self._headers()) as session:
            async with session.get(endpoint, params=params) as response:
                response.raise_for_status()
                payload = await response.json()
        delay = payload.get("delay")
        if not isinstance(delay, (int, float)):
            raise MihomoUnavailable(f"unexpected delay response for {proxy_name}: {payload}")
        return float(delay)
