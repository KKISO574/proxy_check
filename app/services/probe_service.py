from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.core.clash_config import load_clash_nodes
from app.core.config import Settings
from app.probes.mihomo import MihomoClient, MihomoManager, MihomoUnavailable
from app.probes.tcping import socks5_connect
from app.storage import repository
from app.storage.models import Node

logger = logging.getLogger(__name__)


@dataclass
class ProbeRunSummary:
    nodes: int
    results: int
    errors: int


class ProbeService:
    def __init__(
        self,
        settings: Settings,
        session_factory: async_sessionmaker[AsyncSession],
        manager: MihomoManager | None = None,
    ) -> None:
        self.settings = settings
        self.session_factory = session_factory
        self.manager = manager or MihomoManager(settings)
        self._run_lock = asyncio.Lock()

    async def sync_nodes(self, session: AsyncSession) -> list[Node]:
        cfg = self.settings.mihomo
        if not cfg.source_config_path:
            return []
        nodes = load_clash_nodes(cfg.source_config_path)
        listener_ports = {
            node.name: cfg.listener_port_start + index
            for index, node in enumerate(nodes)
        }
        return await repository.upsert_nodes(session, nodes, listener_ports)

    async def ensure_mihomo(self) -> None:
        if self.manager.process and self.manager.process.returncode is None:
            return
        await self.manager.start()

    async def run_once(self) -> ProbeRunSummary:
        if self._run_lock.locked():
            return ProbeRunSummary(nodes=0, results=0, errors=1)

        async with self._run_lock:
            async with self.session_factory() as session:
                nodes = await self.sync_nodes(session)
                await repository.cleanup_old_results(session, self.settings.probe.retention_days)
                if not nodes:
                    return ProbeRunSummary(nodes=0, results=0, errors=0)

            mihomo_error: str | None = None
            try:
                await self.ensure_mihomo()
            except MihomoUnavailable as exc:
                mihomo_error = str(exc)
                logger.warning("mihomo unavailable: %s", mihomo_error)

            client = MihomoClient(
                self.manager.controller_base_url,
                self.manager.secret,
                self.settings.probe.timeout_ms,
            )
            semaphore = asyncio.Semaphore(self.settings.probe.concurrency)
            tasks = [
                self._probe_node(node.id, client, semaphore, mihomo_error=mihomo_error)
                for node in nodes
            ]
            counts = await asyncio.gather(*tasks)
            return ProbeRunSummary(
                nodes=len(nodes),
                results=sum(count[0] for count in counts),
                errors=sum(count[1] for count in counts),
            )

    async def _probe_node(
        self,
        node_id: int,
        client: MihomoClient,
        semaphore: asyncio.Semaphore,
        *,
        mihomo_error: str | None,
    ) -> tuple[int, int]:
        async with semaphore:
            async with self.session_factory() as session:
                node = await repository.get_node(session, node_id)
                if node is None:
                    return 0, 1
                results: list[dict[str, object]] = []
                error_count = 0

                if mihomo_error:
                    results.append(
                        {
                            "metric": "delay",
                            "target": self.settings.probe.delay_url,
                            "latency_ms": None,
                            "success": False,
                            "error": mihomo_error,
                        }
                    )
                    error_count += 1
                else:
                    try:
                        delay = await client.delay(
                            node.name,
                            url=self.settings.probe.delay_url,
                            timeout_ms=self.settings.probe.timeout_ms,
                        )
                        results.append(
                            {
                                "metric": "delay",
                                "target": self.settings.probe.delay_url,
                                "latency_ms": delay,
                                "success": True,
                                "error": None,
                            }
                        )
                    except Exception as exc:
                        results.append(
                            {
                                "metric": "delay",
                                "target": self.settings.probe.delay_url,
                                "latency_ms": None,
                                "success": False,
                                "error": str(exc),
                            }
                        )
                        error_count += 1

                for target in self.settings.probe.tcp_targets:
                    if mihomo_error or node.listener_port is None:
                        results.append(
                            {
                                "metric": "tcping",
                                "target": target.label,
                                "latency_ms": None,
                                "success": False,
                                "error": mihomo_error or "listener port is not assigned",
                            }
                        )
                        error_count += 1
                        continue
                    try:
                        latency = await socks5_connect(
                            self.settings.mihomo.listener_host,
                            node.listener_port,
                            target.host,
                            target.port,
                            timeout_ms=self.settings.probe.timeout_ms,
                        )
                        results.append(
                            {
                                "metric": "tcping",
                                "target": target.label,
                                "latency_ms": latency,
                                "success": True,
                                "error": None,
                            }
                        )
                    except Exception as exc:
                        results.append(
                            {
                                "metric": "tcping",
                                "target": target.label,
                                "latency_ms": None,
                                "success": False,
                                "error": str(exc),
                            }
                        )
                        error_count += 1

                await repository.save_probe_batch(session, node, results)
                await session.commit()
                return len(results), error_count

