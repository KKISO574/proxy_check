from __future__ import annotations

from datetime import timedelta

import pytest
from httpx import ASGITransport, AsyncClient
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from app.main import app
from app.storage.models import Base, Node, NodeMeta, ProbeResult, utcnow


@pytest.mark.asyncio
async def test_nodes_and_history_api():
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
                    success=True,
                    created_at=utcnow() - timedelta(minutes=1),
                ),
                ProbeResult(
                    node_id=node.id,
                    metric="tcping",
                    target="1.1.1.1:443",
                    latency_ms=90,
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
            nodes_response = await client.get("/api/nodes")
            assert nodes_response.status_code == 200
            nodes = nodes_response.json()
            assert nodes[0]["name"] == "node-a"
            assert nodes[0]["latest_delay_ms"] == 120

            history_response = await client.get(f"/api/nodes/{nodes[0]['id']}/history?metric=delay&range=1h")
            assert history_response.status_code == 200
            assert history_response.json()[0]["latency_ms"] == 120
    finally:
        database.SessionLocal = old_session
        await engine.dispose()


@pytest.mark.asyncio
async def test_nodes_api_exposes_metric_summary_map_for_latest_results():
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
                    target="tcping:default",
                    latency_ms=None,
                    value=5.0,
                    data='{"sent":20,"failed":1}',
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
            response = await client.get("/api/nodes")
            assert response.status_code == 200
            row = response.json()[0]
            assert row["metrics"]["delay"]["latency_ms"] == 120
            assert row["metrics"]["delay"]["value"] == 120
            assert row["metrics"]["packet_loss"]["latency_ms"] is None
            assert row["metrics"]["packet_loss"]["value"] == 5.0
            assert row["metrics"]["packet_loss"]["data"] == '{"sent":20,"failed":1}'
            assert row["latest_delay_ms"] == 120
    finally:
        database.SessionLocal = old_session
        await engine.dispose()


@pytest.mark.asyncio
async def test_node_detail_api_exposes_node_meta():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    async with session_factory() as session:
        node = Node(name="node-a", status="available")
        session.add(node)
        await session.flush()
        session.add(
            NodeMeta(
                node_id=node.id,
                exit_ip="203.0.113.10",
                asn="AS64500",
                country="US",
                region="California",
                isp="Example ISP",
            )
        )
        await session.commit()
        node_id = node.id

    from app.storage import database

    old_session = database.SessionLocal
    database.SessionLocal = session_factory
    try:
        transport = ASGITransport(app=app)
        async with AsyncClient(transport=transport, base_url="http://test") as client:
            response = await client.get(f"/api/nodes/{node_id}")
            assert response.status_code == 200
            meta = response.json()["meta"]
            assert meta["exit_ip"] == "203.0.113.10"
            assert meta["asn"] == "AS64500"
            assert meta["country"] == "US"
            assert meta["region"] == "California"
            assert meta["isp"] == "Example ISP"
    finally:
        database.SessionLocal = old_session
        await engine.dispose()
