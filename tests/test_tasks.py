from __future__ import annotations

from datetime import timedelta
from pathlib import Path

import pytest
import yaml
from httpx import ASGITransport, AsyncClient
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from app.core.config import Settings
from app.main import app
from app.services.config_import import ConfigImportError, ConfigImportService
from app.storage import repository
from app.storage.models import Base, MonitorTask, Node, ProbeResult, utcnow


class FakeConfigImportService(ConfigImportService):
    def __init__(self, settings: Settings, payloads: dict[str, str]) -> None:
        super().__init__(settings)
        self.payloads = payloads

    async def fetch_url(self, url: str) -> str:
        if url not in self.payloads:
            raise ConfigImportError("download failed")
        return self.payloads[url]


async def make_session_factory():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
    return engine, session_factory


def task_payload(*nodes: dict[str, object]) -> str:
    return yaml.safe_dump({"proxies": list(nodes)}, allow_unicode=True)


@pytest.mark.asyncio
async def test_import_url_creates_task_config_and_deduplicates_nodes(tmp_path):
    engine, session_factory = await make_session_factory()
    settings = Settings()
    settings.mihomo.work_dir = str(tmp_path / "mihomo")
    service = FakeConfigImportService(
        settings,
        {
            "https://example.com/a.yaml": task_payload(
                {"name": "node-a", "type": "ss", "server": "a.example.com", "port": 443},
                {"name": "node-a", "type": "ss", "server": "dup.example.com", "port": 443},
                {"name": "node-b", "type": "vmess", "server": "b.example.com", "port": 8443},
            )
        },
    )

    try:
        async with session_factory() as session:
            task, nodes = await service.create_task_from_url(
                session,
                name="主线路",
                source_url="https://example.com/a.yaml",
                interval_seconds=90,
            )

            assert task.id is not None
            assert task.name == "主线路"
            assert task.enabled is True
            assert task.interval_seconds == 90
            assert task.config_path.endswith(f"task-{task.id}.yaml")
            assert Path(task.config_path).exists()
            assert [node.name for node in nodes] == ["node-a", "node-b"]
            assert nodes[0].task_id == task.id
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_import_url_rejects_invalid_scheme_without_writing_config(tmp_path):
    engine, session_factory = await make_session_factory()
    settings = Settings()
    settings.mihomo.work_dir = str(tmp_path / "mihomo")
    service = FakeConfigImportService(settings, {})

    try:
        async with session_factory() as session:
            with pytest.raises(ConfigImportError, match="http"):
                await service.create_task_from_url(
                    session,
                    name="bad",
                    source_url="file:///tmp/config.yaml",
                    interval_seconds=60,
                )

            assert list((tmp_path / "mihomo").glob("**/*.yaml")) == []
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_refresh_failure_keeps_existing_config_and_nodes(tmp_path):
    engine, session_factory = await make_session_factory()
    settings = Settings()
    settings.mihomo.work_dir = str(tmp_path / "mihomo")
    service = FakeConfigImportService(
        settings,
        {
            "https://example.com/a.yaml": task_payload(
                {"name": "node-a", "type": "ss", "server": "a.example.com", "port": 443}
            )
        },
    )

    try:
        async with session_factory() as session:
            task, _ = await service.create_task_from_url(
                session,
                name="任务",
                source_url="https://example.com/a.yaml",
                interval_seconds=60,
            )
            original_config = Path(task.config_path).read_text(encoding="utf-8")
            service.payloads["https://example.com/a.yaml"] = "not: [valid"

            with pytest.raises(ConfigImportError):
                await service.refresh_task(session, task.id)

            refreshed = await repository.get_task(session, task.id)
            assert refreshed is not None
            assert Path(refreshed.config_path).read_text(encoding="utf-8") == original_config
            nodes = await repository.list_nodes(session, task_id=task.id)
            assert [node.name for node in nodes] == ["node-a"]
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_task_api_filters_nodes_and_stats_by_task():
    engine, session_factory = await make_session_factory()
    async with session_factory() as session:
        task_a = MonitorTask(name="A", source_url="https://a.example/config.yaml", config_path="/tmp/a.yaml")
        task_b = MonitorTask(name="B", source_url="https://b.example/config.yaml", config_path="/tmp/b.yaml")
        session.add_all([task_a, task_b])
        await session.flush()
        node_a = Node(
            task_id=task_a.id,
            name="same",
            type="ss",
            server="a.example.com",
            port=443,
            listener_port=20000,
            status="available",
            last_checked_at=utcnow(),
        )
        node_b = Node(
            task_id=task_b.id,
            name="same",
            type="ss",
            server="b.example.com",
            port=443,
            listener_port=20100,
            status="down",
            last_checked_at=utcnow(),
        )
        session.add_all([node_a, node_b])
        await session.flush()
        session.add(
            ProbeResult(
                node_id=node_a.id,
                metric="delay",
                target="https://cp.cloudflare.com/generate_204",
                latency_ms=100,
                success=True,
                created_at=utcnow() - timedelta(minutes=1),
            )
        )
        await session.commit()

    from app.storage import database

    old_session = database.SessionLocal
    database.SessionLocal = session_factory
    try:
        transport = ASGITransport(app=app)
        async with AsyncClient(transport=transport, base_url="http://test") as client:
            tasks_response = await client.get("/api/tasks")
            assert tasks_response.status_code == 200
            tasks = tasks_response.json()
            assert [task["name"] for task in tasks] == ["A", "B"]
            assert tasks[0]["node_count"] == 1

            nodes_response = await client.get(f"/api/nodes?task_id={task_a.id}")
            assert nodes_response.status_code == 200
            nodes = nodes_response.json()
            assert len(nodes) == 1
            assert nodes[0]["task_id"] == task_a.id
            assert nodes[0]["server"] == "a.example.com"

            stats_response = await client.get(f"/api/stats?task_id={task_a.id}")
            assert stats_response.status_code == 200
            assert stats_response.json()["total_nodes"] == 1
            assert stats_response.json()["available_nodes"] == 1
    finally:
        database.SessionLocal = old_session
        await engine.dispose()


@pytest.mark.asyncio
async def test_delete_task_removes_nodes_and_probe_results():
    engine, session_factory = await make_session_factory()
    async with session_factory() as session:
        task = MonitorTask(name="A", source_url="https://a.example/config.yaml", config_path="/tmp/a.yaml")
        session.add(task)
        await session.flush()
        node = Node(task_id=task.id, name="node-a", status="available")
        session.add(node)
        await session.flush()
        session.add(
            ProbeResult(
                node_id=node.id,
                metric="delay",
                target="https://cp.cloudflare.com/generate_204",
                latency_ms=100,
                success=True,
            )
        )
        await session.commit()

        await repository.delete_task(session, task)

        assert await repository.list_tasks(session) == []
        assert await repository.list_nodes(session) == []
        rows = await session.execute(ProbeResult.__table__.select())
        assert rows.all() == []

    await engine.dispose()
