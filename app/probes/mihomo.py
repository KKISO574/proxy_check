from __future__ import annotations

import asyncio
import logging
import os
from pathlib import Path
from urllib.parse import quote

import aiohttp

from app.core.clash_config import build_runtime_config, load_clash_nodes
from app.core.config import Settings

logger = logging.getLogger(__name__)


class MihomoUnavailable(RuntimeError):
    pass


class MihomoManager:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.process: asyncio.subprocess.Process | None = None
        self.runtime_config_path = Path(settings.mihomo.work_dir) / "config.yaml"
        self.listener_ports: dict[str, int] = {}
        self.active_config_path: str | None = None
        self._stream_tasks: list[asyncio.Task[None]] = []

    @property
    def controller_base_url(self) -> str:
        cfg = self.settings.mihomo
        return f"http://{cfg.controller_host}:{cfg.controller_port}"

    @property
    def secret(self) -> str:
        return os.environ.get(self.settings.mihomo.secret_env, "")

    async def prepare(
        self,
        source_config_path: str | None = None,
        listener_ports: dict[str, int] | None = None,
        listener_port_start: int | None = None,
    ) -> None:
        cfg = self.settings.mihomo
        config_path = source_config_path or cfg.source_config_path
        if not config_path:
            raise MihomoUnavailable("mihomo.source_config_path is not configured")
        if not Path(config_path).exists():
            raise MihomoUnavailable(f"source config not found: {config_path}")
        if not self.secret:
            raise MihomoUnavailable(f"environment variable {cfg.secret_env} is not set")

        nodes = load_clash_nodes(config_path)
        if listener_ports:
            port_map = {node.name: listener_ports[node.name] for node in nodes if node.name in listener_ports}
        else:
            start = listener_port_start or cfg.listener_port_start
            port_map = {node.name: start + index for index, node in enumerate(nodes)}
        self.listener_ports = build_runtime_config(
            config_path,
            self.runtime_config_path,
            controller_host=cfg.controller_host,
            controller_port=cfg.controller_port,
            secret=self.secret,
            listener_host=cfg.listener_host,
            listener_ports=port_map,
        )
        self.active_config_path = str(config_path)

    async def start(
        self,
        source_config_path: str | None = None,
        listener_ports: dict[str, int] | None = None,
        listener_port_start: int | None = None,
    ) -> None:
        cfg = self.settings.mihomo
        if not cfg.bin:
            raise MihomoUnavailable("mihomo.bin is not configured")
        if not Path(cfg.bin).exists():
            raise MihomoUnavailable(f"mihomo binary not found: {cfg.bin}")
        config_path = str(source_config_path or cfg.source_config_path)
        if (
            self.process
            and self.process.returncode is None
            and self.active_config_path == config_path
        ):
            return
        if self.process and self.process.returncode is None:
            await self.stop()
        await self.prepare(source_config_path, listener_ports, listener_port_start)
        self.process = await asyncio.create_subprocess_exec(
            cfg.bin,
            "-f",
            str(self.runtime_config_path),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        self._start_stream_consumers()
        try:
            await self._wait_ready()
        except Exception:
            await self.stop()
            raise

    def _start_stream_consumers(self) -> None:
        self._stream_tasks = []
        if self.process is None:
            return
        if self.process.stdout is not None:
            self._stream_tasks.append(
                asyncio.create_task(self._consume_stream(self.process.stdout, "stdout", logging.INFO))
            )
        if self.process.stderr is not None:
            self._stream_tasks.append(
                asyncio.create_task(self._consume_stream(self.process.stderr, "stderr", logging.WARNING))
            )

    async def _consume_stream(
        self,
        stream: asyncio.StreamReader,
        stream_name: str,
        level: int,
    ) -> None:
        try:
            while True:
                line = await stream.readline()
                if not line:
                    return
                logger.log(
                    level,
                    "mihomo %s: %s",
                    stream_name,
                    line.decode("utf-8", errors="replace").rstrip(),
                )
        except asyncio.CancelledError:
            raise
        except Exception:
            logger.exception("failed to consume mihomo %s", stream_name)

    async def _finish_stream_consumers(self) -> None:
        if not self._stream_tasks:
            return
        done, pending = await asyncio.wait(self._stream_tasks, timeout=1)
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*pending, return_exceptions=True)
        for task in done:
            task.result()
        self._stream_tasks = []

    async def _wait_ready(self, *, attempts: int = 30, interval_seconds: float = 0.1) -> None:
        endpoint = f"{self.controller_base_url}/version"
        request_timeout = aiohttp.ClientTimeout(total=interval_seconds)
        headers = {"Authorization": f"Bearer {self.secret}"} if self.secret else {}
        last_error: BaseException | None = None
        async with aiohttp.ClientSession(timeout=request_timeout, headers=headers) as session:
            for _ in range(attempts):
                if self.process is not None and self.process.returncode is not None:
                    raise MihomoUnavailable(
                        f"mihomo exited before ready (rc={self.process.returncode})"
                    )
                try:
                    async with session.get(endpoint) as response:
                        if response.status == 200:
                            return
                except Exception as exc:  # network errors, timeouts
                    last_error = exc
                await asyncio.sleep(interval_seconds)
        total_seconds = attempts * interval_seconds
        detail = f": {last_error}" if last_error else ""
        raise MihomoUnavailable(
            f"controller not ready within {total_seconds:.1f}s{detail}"
        )

    async def stop(self) -> None:
        if not self.process:
            await self._finish_stream_consumers()
            return
        if self.process.returncode is None:
            self.process.terminate()
            try:
                await asyncio.wait_for(self.process.wait(), timeout=5)
            except TimeoutError:
                self.process.kill()
                await self.process.wait()
        await self._finish_stream_consumers()
        self.process = None


class MihomoClient:
    def __init__(self, base_url: str, secret: str, timeout_ms: int) -> None:
        self.base_url = base_url.rstrip("/")
        self.secret = secret
        self.timeout = aiohttp.ClientTimeout(total=timeout_ms / 1000)
        self._session: aiohttp.ClientSession | None = None

    def _headers(self) -> dict[str, str]:
        if not self.secret:
            return {}
        return {"Authorization": f"Bearer {self.secret}"}

    async def _get_session(self) -> aiohttp.ClientSession:
        if self._session is None or self._session.closed:
            self._session = aiohttp.ClientSession(timeout=self.timeout, headers=self._headers())
        return self._session

    async def aclose(self) -> None:
        if self._session is not None and not self._session.closed:
            await self._session.close()
        self._session = None

    async def __aenter__(self) -> "MihomoClient":
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.aclose()

    async def delay(self, proxy_name: str, *, url: str, timeout_ms: int) -> float:
        endpoint = f"{self.base_url}/proxies/{quote(proxy_name, safe='')}/delay"
        params = {"url": url, "timeout": str(timeout_ms)}
        session = await self._get_session()
        async with session.get(endpoint, params=params) as response:
            response.raise_for_status()
            payload = await response.json()
        delay = payload.get("delay")
        if not isinstance(delay, (int, float)):
            raise MihomoUnavailable(f"unexpected delay response for {proxy_name}: {payload}")
        return float(delay)
