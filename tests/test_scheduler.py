from __future__ import annotations

from datetime import timedelta

import pytest
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from app.scheduler.runner import ProbeScheduler
from app.storage.models import Base, MonitorTask, utcnow


class FakeProbeService:
    def __init__(self, session_factory):
        self.session_factory = session_factory
        self.runs: list[int] = []

    async def run_task(self, task_id: int):
        self.runs.append(task_id)


@pytest.mark.asyncio
async def test_scheduler_runs_due_enabled_tasks_only():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    now = utcnow()
    async with session_factory() as session:
        due = MonitorTask(
            name="due",
            source_url="https://example.com/a.yaml",
            config_path="/tmp/a.yaml",
            enabled=True,
            interval_seconds=60,
            next_run_at=now - timedelta(seconds=1),
        )
        later = MonitorTask(
            name="later",
            source_url="https://example.com/b.yaml",
            config_path="/tmp/b.yaml",
            enabled=True,
            interval_seconds=60,
            next_run_at=now + timedelta(minutes=5),
        )
        paused = MonitorTask(
            name="paused",
            source_url="https://example.com/c.yaml",
            config_path="/tmp/c.yaml",
            enabled=False,
            interval_seconds=60,
            next_run_at=now - timedelta(seconds=1),
        )
        session.add_all([due, later, paused])
        await session.commit()

    try:
        service = FakeProbeService(session_factory)
        scheduler = ProbeScheduler(service, interval_seconds=60)  # type: ignore[arg-type]

        await scheduler.run_due_tasks()

        assert service.runs == [1]
    finally:
        await engine.dispose()
