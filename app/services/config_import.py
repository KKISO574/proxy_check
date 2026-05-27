from __future__ import annotations

from pathlib import Path
from urllib.parse import urlparse

import aiohttp
import yaml
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.clash_config import load_clash_nodes
from app.core.config import Settings
from app.services.port_allocator import allocate_for_task
from app.storage import repository
from app.storage.models import MonitorTask, Node, utcnow


class ConfigImportError(RuntimeError):
    pass


class ConfigImportService:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings

    def config_dir(self) -> Path:
        path = Path(self.settings.mihomo.imported_config_dir)
        path.mkdir(parents=True, exist_ok=True)
        return path

    def validate_url(self, url: str) -> None:
        parsed = urlparse(url)
        if parsed.scheme not in {"http", "https"}:
            raise ConfigImportError("only http/https Clash config URLs are supported")
        if not parsed.netloc:
            raise ConfigImportError("config URL host is required")

    async def fetch_url(self, url: str) -> str:
        self.validate_url(url)
        timeout = aiohttp.ClientTimeout(total=self.settings.probe.import_timeout_ms / 1000)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            async with session.get(url) as response:
                response.raise_for_status()
                return await response.text()

    def validate_yaml(self, content: str) -> None:
        try:
            data = yaml.safe_load(content) or {}
        except yaml.YAMLError as exc:
            raise ConfigImportError(f"invalid YAML: {exc}") from exc
        if not isinstance(data, dict):
            raise ConfigImportError("Clash config must be a YAML object")
        if not isinstance(data.get("proxies"), list):
            raise ConfigImportError("Clash config must contain a proxies list")

    def task_config_path(self, task_id: int) -> Path:
        return self.config_dir() / f"task-{task_id}.yaml"

    def write_task_config(self, task_id: int, content: str) -> Path:
        self.validate_yaml(content)
        path = self.task_config_path(task_id)
        tmp_path = path.with_suffix(".yaml.tmp")
        tmp_path.write_text(content, encoding="utf-8")
        tmp_path.replace(path)
        return path

    async def create_task_from_url(
        self,
        session: AsyncSession,
        *,
        name: str,
        source_url: str,
        interval_seconds: int,
        enabled: bool = True,
    ) -> tuple[MonitorTask, list[Node]]:
        self.validate_url(source_url)
        content = await self.fetch_url(source_url)
        self.validate_yaml(content)
        task = await repository.create_task(
            session,
            name=name,
            source_url=source_url,
            config_path="",
            interval_seconds=interval_seconds,
            enabled=enabled,
        )
        config_path = self.write_task_config(task.id, content)
        nodes = load_clash_nodes(config_path)
        listener_ports = await allocate_for_task(
            session,
            task_id=task.id,
            desired_names=[node.name for node in nodes],
            port_start=self.settings.mihomo.listener_port_start,
            port_max=self.settings.mihomo.listener_port_max,
        )
        synced = await repository.upsert_nodes(session, nodes, listener_ports, task_id=task.id)
        await repository.update_task(
            session,
            task,
            config_path=str(config_path),
            status="unknown",
            last_refresh_at=utcnow(),
            last_refresh_error=None,
        )
        return task, synced

    async def refresh_task(self, session: AsyncSession, task_id: int) -> tuple[MonitorTask, list[Node]]:
        task = await repository.get_task(session, task_id)
        if task is None:
            raise ConfigImportError("task not found")
        content = await self.fetch_url(task.source_url)
        self.validate_yaml(content)
        config_path = self.write_task_config(task.id, content)
        nodes = load_clash_nodes(config_path)
        listener_ports = await allocate_for_task(
            session,
            task_id=task.id,
            desired_names=[node.name for node in nodes],
            port_start=self.settings.mihomo.listener_port_start,
            port_max=self.settings.mihomo.listener_port_max,
        )
        synced = await repository.upsert_nodes(session, nodes, listener_ports, task_id=task.id)
        task = await repository.update_task(
            session,
            task,
            config_path=str(config_path),
            status="unknown",
            last_refresh_at=utcnow(),
            last_refresh_error=None,
        )
        return task, synced
