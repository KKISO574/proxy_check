from __future__ import annotations

from datetime import timedelta

import pytest
from httpx import ASGITransport, AsyncClient
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from app.main import app
from app.storage.models import Base, Node, ProbeResult, utcnow


@pytest.mark.asyncio
async def test_prometheus_metrics_endpoint_exports_node_metrics_and_score() -> None:
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    async with session_factory() as session:
        node = Node(
            name="node-a",
            type="ss",
            server="a.example.com",
            port=443,
            listener_port=20000,
            status="available",
            last_checked_at=utcnow(),
        )
        session.add(node)
        await session.flush()
        session.add_all(
            [
                ProbeResult(
                    node_id=node.id,
                    metric="delay",
                    target="https://cp.cloudflare.com/generate_204",
                    latency_ms=120,
                    value=120,
                    success=True,
                    created_at=utcnow() - timedelta(minutes=2),
                ),
                ProbeResult(
                    node_id=node.id,
                    metric="packet_loss",
                    target="1.1.1.1:443",
                    value=5,
                    success=True,
                    created_at=utcnow() - timedelta(minutes=1),
                ),
            ]
        )
        await session.commit()

    from app.storage import database

    old_session = database.SessionLocal
    database.SessionLocal = session_factory
    try:
        transport = ASGITransport(app=app)
        async with AsyncClient(transport=transport, base_url="http://test") as client:
            response = await client.get("/metrics")
    finally:
        database.SessionLocal = old_session
        await engine.dispose()

    assert response.status_code == 200
    assert response.headers["content-type"].startswith("text/plain")
    body = response.text
    assert "# TYPE proxy_check_node_score gauge" in body
    assert 'proxy_check_node_score{node_id="1",node_name="node-a",task_id="",status="available"}' in body
    assert 'proxy_check_node_availability{node_id="1",node_name="node-a",task_id="",status="available"} 1' in body
    assert 'proxy_check_node_metric_latency_ms{node_id="1",node_name="node-a",task_id="",status="available",metric="delay",target="https://cp.cloudflare.com/generate_204"} 120.0' in body
    assert 'proxy_check_node_metric_value{node_id="1",node_name="node-a",task_id="",status="available",metric="packet_loss",target="1.1.1.1:443"} 5.0' in body


@pytest.mark.asyncio
async def test_prometheus_metrics_endpoint_exports_empty_document() -> None:
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    from app.storage import database

    old_session = database.SessionLocal
    database.SessionLocal = session_factory
    try:
        transport = ASGITransport(app=app)
        async with AsyncClient(transport=transport, base_url="http://test") as client:
            response = await client.get("/metrics")
    finally:
        database.SessionLocal = old_session
        await engine.dispose()

    assert response.status_code == 200
    assert "# HELP proxy_check_node_score Computed node quality score." in response.text
    assert "proxy_check_node_score{" not in response.text
