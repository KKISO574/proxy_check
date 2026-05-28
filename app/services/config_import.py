from __future__ import annotations

import ipaddress
import socket
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


# Properties that mark an IP as not safely reachable from a public service.
# `is_private` covers RFC1918 + 127.0.0.0/8 + RFC4193 unique-local IPv6;
# `is_loopback` is redundant with `is_private` on modern Python but kept for clarity;
# `is_link_local` blocks 169.254.0.0/16 (incl. cloud metadata 169.254.169.254) and fe80::/10;
# `is_reserved` blocks 240.0.0.0/4 and other IETF-reserved ranges.
_BLOCKED_IP_PROPERTIES: tuple[str, ...] = (
    "is_private",
    "is_loopback",
    "is_link_local",
    "is_reserved",
)


def _resolve_and_validate_host(host: str) -> list[str]:
    """Resolve ``host`` to every A/AAAA address and reject private/internal IPs.

    Used by :class:`ConfigImportService` to defend against SSRF attacks where a
    user-supplied subscription URL points at a private network (cloud metadata,
    Docker bridge, internal services). DNS rebinding remains a residual concern
    — callers must not expect the resolution result to match what aiohttp will
    later resolve, so this is a best-effort guard, not a guarantee.
    """
    if not host:
        raise ConfigImportError("config URL host is required")
    try:
        infos = socket.getaddrinfo(host, None)
    except socket.gaierror as exc:
        raise ConfigImportError(f"failed to resolve host {host!r}: {exc}") from exc
    addresses: list[str] = []
    for info in infos:
        sockaddr = info[4]
        ip_str = sockaddr[0]
        # IPv6 scoped addresses look like "fe80::1%eth0"; strip the zone for parsing.
        ip_for_parse = ip_str.split("%", 1)[0]
        try:
            ip = ipaddress.ip_address(ip_for_parse)
        except ValueError as exc:
            raise ConfigImportError(f"unparseable address {ip_str!r}: {exc}") from exc
        if any(getattr(ip, prop) for prop in _BLOCKED_IP_PROPERTIES):
            raise ConfigImportError("private/internal addresses are blocked")
        addresses.append(ip_str)
    if not addresses:
        raise ConfigImportError(f"no addresses resolved for host {host!r}")
    return addresses


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
        parsed = urlparse(url)
        _resolve_and_validate_host(parsed.hostname or "")
        timeout = aiohttp.ClientTimeout(total=self.settings.probe.import_timeout_ms / 1000)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            async with session.get(url, allow_redirects=False) as response:
                if 300 <= response.status < 400:
                    # We refuse to follow redirects ourselves: a 301 to a
                    # private IP would slip past the SSRF guard above. Ask
                    # the user to provide the final URL directly.
                    raise ConfigImportError(
                        f"redirects are not followed (status={response.status}); "
                        "submit the final URL directly"
                    )
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
