from __future__ import annotations

import pytest
from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

from app.storage.database import _upgrade_sqlite_schema


@pytest.mark.asyncio
async def test_upgrade_rebuilds_legacy_global_unique_nodes_table():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    async with engine.begin() as conn:
        await conn.execute(text("CREATE TABLE monitor_tasks (id INTEGER PRIMARY KEY, name VARCHAR(255), source_url TEXT, config_path TEXT, enabled BOOLEAN, interval_seconds INTEGER, status VARCHAR(32), last_refresh_at DATETIME, last_refresh_error TEXT, last_checked_at DATETIME, next_run_at DATETIME, created_at DATETIME, updated_at DATETIME)"))
        await conn.execute(text("CREATE TABLE nodes (id INTEGER PRIMARY KEY, name VARCHAR(255) UNIQUE, type VARCHAR(64), server VARCHAR(255), port INTEGER, listener_port INTEGER, status VARCHAR(32), last_checked_at DATETIME, created_at DATETIME, updated_at DATETIME)"))
        await conn.execute(text("INSERT INTO nodes (id, name, status) VALUES (1, 'node-a', 'unknown')"))

        await _upgrade_sqlite_schema(conn)

        indexes = (await conn.execute(text("PRAGMA index_list(nodes)"))).mappings().all()
        unique_columns: list[list[str]] = []
        for index in indexes:
            if index["unique"]:
                columns = (await conn.execute(text(f"PRAGMA index_info({index['name']})"))).mappings().all()
                unique_columns.append([column["name"] for column in columns])
        assert ["name"] not in unique_columns
        assert ["task_id", "name"] in unique_columns

        row = (await conn.execute(text("SELECT task_id, name FROM nodes WHERE id = 1"))).mappings().one()
        assert row["task_id"] == 1
        assert row["name"] == "node-a"

    await engine.dispose()


@pytest.mark.asyncio
async def test_upgrade_drops_legacy_named_index_before_recreating_nodes_table():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    async with engine.begin() as conn:
        await conn.execute(text("CREATE TABLE monitor_tasks (id INTEGER PRIMARY KEY, name VARCHAR(255), source_url TEXT, config_path TEXT, enabled BOOLEAN, interval_seconds INTEGER, status VARCHAR(32), last_refresh_at DATETIME, last_refresh_error TEXT, last_checked_at DATETIME, next_run_at DATETIME, created_at DATETIME, updated_at DATETIME)"))
        await conn.execute(text("CREATE TABLE nodes (id INTEGER PRIMARY KEY, name VARCHAR(255) UNIQUE, type VARCHAR(64), server VARCHAR(255), port INTEGER, listener_port INTEGER, status VARCHAR(32), last_checked_at DATETIME, created_at DATETIME, updated_at DATETIME)"))
        await conn.execute(text("CREATE INDEX ix_nodes_name ON nodes (name)"))
        await conn.execute(text("INSERT INTO nodes (id, name, status) VALUES (1, 'node-a', 'unknown')"))

        await _upgrade_sqlite_schema(conn)

        indexes = (await conn.execute(text("PRAGMA index_list(nodes)"))).mappings().all()
        assert any(index["name"] == "ix_nodes_name" for index in indexes)
        row = (await conn.execute(text("SELECT task_id, name FROM nodes WHERE id = 1"))).mappings().one()
        assert row["task_id"] == 1
        assert row["name"] == "node-a"

    await engine.dispose()


@pytest.mark.asyncio
async def test_upgrade_adds_v2_metric_columns_and_node_meta_table():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    async with engine.begin() as conn:
        await conn.execute(text("CREATE TABLE monitor_tasks (id INTEGER PRIMARY KEY, name VARCHAR(255), source_url TEXT, config_path TEXT, enabled BOOLEAN, interval_seconds INTEGER, status VARCHAR(32), last_refresh_at DATETIME, last_refresh_error TEXT, last_checked_at DATETIME, next_run_at DATETIME, created_at DATETIME, updated_at DATETIME)"))
        await conn.execute(text("CREATE TABLE nodes (id INTEGER PRIMARY KEY, task_id INTEGER, name VARCHAR(255), type VARCHAR(64), server VARCHAR(255), port INTEGER, listener_port INTEGER, status VARCHAR(32), last_checked_at DATETIME, created_at DATETIME, updated_at DATETIME)"))
        await conn.execute(text("CREATE TABLE probe_results (id INTEGER PRIMARY KEY, node_id INTEGER, metric VARCHAR(32), target VARCHAR(255), latency_ms FLOAT, success BOOLEAN, error TEXT, created_at DATETIME)"))

        await _upgrade_sqlite_schema(conn)

        probe_columns = {
            column["name"]
            for column in (await conn.execute(text("PRAGMA table_info(probe_results)"))).mappings().all()
        }
        assert {"value", "data"}.issubset(probe_columns)

        tables = {
            row["name"]
            for row in (await conn.execute(text("SELECT name FROM sqlite_master WHERE type = 'table'"))).mappings().all()
        }
        assert "node_meta" in tables

    await engine.dispose()
