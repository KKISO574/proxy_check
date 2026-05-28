from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass
from datetime import datetime, timedelta

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.core.clash_config import load_clash_nodes
from app.core.config import Settings
from app.probes.base import ProbeContext, ProbeOutcome
from app.probes.builtin import (
    DelayProber,
    ExitIpGeoProber,
    HttpRttProber,
    JitterProber,
    PacketLossProber,
    TcpingProber,
    TlsHandshakeProber,
)
from app.probes.mihomo import MihomoClient, MihomoManager, MihomoUnavailable
from app.probes.registry import ProbeRegistry
from app.services.port_allocator import allocate_for_task
from app.storage import repository
from app.storage.models import MonitorTask, Node, utcnow

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
        registry: ProbeRegistry | None = None,
    ) -> None:
        self.settings = settings
        self.session_factory = session_factory
        self.manager = manager or MihomoManager(settings)
        self.registry = registry or ProbeRegistry(
            [
                DelayProber(),
                TcpingProber(),
                TlsHandshakeProber(),
                HttpRttProber(),
                JitterProber(session_factory),
                PacketLossProber(),
                ExitIpGeoProber(session_factory),
            ]
        )
        self._run_lock = asyncio.Lock()

    async def sync_nodes(self, session: AsyncSession, task: MonitorTask | None = None) -> list[Node]:
        cfg = self.settings.mihomo
        source_config_path = task.config_path if task is not None else cfg.source_config_path
        if not source_config_path:
            return []
        nodes = load_clash_nodes(source_config_path)
        listener_ports = await allocate_for_task(
            session,
            task_id=task.id if task is not None else None,
            desired_names=[node.name for node in nodes],
            port_start=cfg.listener_port_start,
            port_max=cfg.listener_port_max,
        )
        return await repository.upsert_nodes(
            session,
            nodes,
            listener_ports,
            task_id=task.id if task is not None else None,
        )

    async def ensure_mihomo(self, task: MonitorTask | None = None) -> None:
        cfg = self.settings.mihomo
        source_config_path = task.config_path if task is not None else cfg.source_config_path
        async with self.session_factory() as session:
            nodes = await repository.list_nodes(
                session, task_id=task.id if task is not None else None
            )
        listener_ports = {
            node.name: int(node.listener_port)
            for node in nodes
            if node.listener_port is not None and node.status != "removed"
        }
        await self.manager.start(
            source_config_path,
            listener_ports=listener_ports or None,
            listener_port_start=cfg.listener_port_start,
        )

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

    async def run_task(self, task_id: int) -> ProbeRunSummary:
        if self._run_lock.locked():
            return ProbeRunSummary(nodes=0, results=0, errors=1)

        async with self._run_lock:
            async with self.session_factory() as session:
                task = await repository.get_task(session, task_id)
                if task is None or not task.enabled:
                    return ProbeRunSummary(nodes=0, results=0, errors=1)
                nodes = await self.sync_nodes(session, task)
                await repository.cleanup_old_results(session, self.settings.probe.retention_days)
                if not nodes:
                    now = utcnow()
                    await repository.update_task(
                        session,
                        task,
                        status="unknown",
                        last_checked_at=now,
                        next_run_at=now + timedelta(seconds=task.interval_seconds),
                    )
                    return ProbeRunSummary(nodes=0, results=0, errors=0)

            mihomo_error: str | None = None
            try:
                await self.ensure_mihomo(task)
            except MihomoUnavailable as exc:
                mihomo_error = str(exc)
                logger.warning("mihomo unavailable: %s", mihomo_error)

            client = MihomoClient(
                self.manager.controller_base_url,
                self.manager.secret,
                self.settings.probe.timeout_ms,
            )
            semaphore = asyncio.Semaphore(self.settings.probe.concurrency)
            counts = await asyncio.gather(
                *[
                    self._probe_node(node.id, client, semaphore, mihomo_error=mihomo_error)
                    for node in nodes
                ]
            )
            async with self.session_factory() as session:
                fresh_task = await repository.get_task(session, task_id)
                if fresh_task is not None:
                    now = utcnow()
                    status = "available" if any(count[0] > count[1] for count in counts) else "down"
                    await repository.update_task(
                        session,
                        fresh_task,
                        status=status,
                        last_checked_at=now,
                        next_run_at=now + timedelta(seconds=fresh_task.interval_seconds),
                    )
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
                last_seen = await repository.last_metric_timestamps(session, node_id)
                results = await self._run_probers(node, client, mihomo_error, last_seen)
                error_count = len([result for result in results if not result.success])

                await repository.save_probe_batch(
                    session,
                    node,
                    [
                        {
                            "metric": result.metric,
                            "target": result.target,
                            "latency_ms": result.latency_ms,
                            "value": result.value,
                            "data": result.data,
                            "success": result.success,
                            "error": result.error,
                        }
                        for result in results
                    ],
                )
                await session.commit()
                return len(results), error_count

    async def _run_probers(
        self,
        node: Node,
        client: MihomoClient | None,
        mihomo_error: str | None,
        last_seen: dict[str, "datetime"] | None = None,
    ) -> list[ProbeOutcome]:
        context = ProbeContext(
            node=node,
            settings=self.settings,
            client=client,
            mihomo_error=mihomo_error,
        )
        results: list[ProbeOutcome] = []
        probers = self.registry.due(
            self.settings.probe.dimensions,
            last_seen or {},
        )
        for prober in probers:
            results.extend(await prober.probe(context))
        return results
