from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timedelta, timezone

from app.services.probe_service import ProbeService
from app.storage import repository
from app.storage.models import utcnow

logger = logging.getLogger(__name__)


def aware(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value


class ProbeScheduler:
    def __init__(self, service: ProbeService, interval_seconds: int) -> None:
        self.service = service
        self.interval_seconds = interval_seconds
        self._task: asyncio.Task[None] | None = None
        self._stopping = asyncio.Event()

    def start(self) -> None:
        if self._task is None or self._task.done():
            self._stopping.clear()
            self._task = asyncio.create_task(self._loop())

    async def stop(self) -> None:
        self._stopping.set()
        if self._task is not None:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass

    async def _loop(self) -> None:
        while not self._stopping.is_set():
            try:
                await self.run_due_tasks()
            except Exception:
                logger.exception("probe run failed")
            try:
                await asyncio.wait_for(self._stopping.wait(), timeout=min(self.interval_seconds, 5))
            except TimeoutError:
                continue

    async def run_due_tasks(self) -> None:
        now = utcnow()
        async with self.service.session_factory() as session:
            tasks = await repository.list_tasks(session)
        for task in tasks:
            if not task.enabled:
                continue
            if task.next_run_at is not None and aware(task.next_run_at) > now:
                continue
            await self.service.run_task(task.id)
            async with self.service.session_factory() as session:
                fresh = await repository.get_task(session, task.id)
                if fresh is not None and fresh.next_run_at is None:
                    await repository.update_task(
                        session,
                        fresh,
                        next_run_at=now + timedelta(seconds=fresh.interval_seconds),
                    )
