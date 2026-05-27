from __future__ import annotations

from collections.abc import AsyncGenerator
from pathlib import Path

from sqlalchemy import inspect, text
from sqlalchemy.ext.asyncio import AsyncEngine, AsyncSession, async_sessionmaker, create_async_engine

from app.core.config import Settings, get_settings
from app.storage.models import Base, utcnow

engine: AsyncEngine | None = None
SessionLocal: async_sessionmaker[AsyncSession] | None = None


def _ensure_sqlite_parent(database_url: str) -> None:
    prefix = "sqlite+aiosqlite:///"
    if not database_url.startswith(prefix):
        return
    db_path = database_url.removeprefix(prefix)
    if db_path and db_path != ":memory:":
        Path(db_path).parent.mkdir(parents=True, exist_ok=True)


def init_engine(settings: Settings | None = None) -> None:
    global engine, SessionLocal
    settings = settings or get_settings()
    _ensure_sqlite_parent(settings.app.database_url)
    engine = create_async_engine(settings.app.database_url, future=True)
    SessionLocal = async_sessionmaker(engine, expire_on_commit=False)


async def init_db() -> None:
    if engine is None:
        init_engine()
    assert engine is not None
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
        await _upgrade_sqlite_schema(conn)


async def _upgrade_sqlite_schema(conn) -> None:
    def inspect_schema(sync_conn):
        inspector = inspect(sync_conn)
        tables = set(inspector.get_table_names())
        columns = {
            table: {column["name"] for column in inspector.get_columns(table)}
            for table in tables
        }
        return tables, columns

    tables, columns = await conn.run_sync(inspect_schema)
    if "nodes" not in tables:
        return
    await _rebuild_legacy_nodes_table(conn)
    tables, columns = await conn.run_sync(inspect_schema)
    if "task_id" not in columns.get("nodes", set()):
        await conn.execute(text("ALTER TABLE nodes ADD COLUMN task_id INTEGER"))
    if "monitor_tasks" not in tables:
        return

    result = await conn.execute(text("SELECT COUNT(*) FROM monitor_tasks"))
    task_count = int(result.scalar_one())
    if task_count == 0:
        now = utcnow().isoformat()
        await conn.execute(
            text(
                """
                INSERT INTO monitor_tasks
                    (name, source_url, config_path, enabled, interval_seconds, status, created_at, updated_at)
                VALUES
                    (:name, :source_url, :config_path, 1, :interval_seconds, 'unknown', :created_at, :updated_at)
                """
            ),
            {
                "name": "默认配置",
                "source_url": "local://legacy",
                "config_path": "",
                "interval_seconds": 60,
                "created_at": now,
                "updated_at": now,
            },
        )
    result = await conn.execute(text("SELECT id FROM monitor_tasks ORDER BY id ASC LIMIT 1"))
    default_task_id = result.scalar_one_or_none()
    if default_task_id is not None:
        await conn.execute(
            text("UPDATE nodes SET task_id = :task_id WHERE task_id IS NULL"),
            {"task_id": int(default_task_id)},
        )


async def _rebuild_legacy_nodes_table(conn) -> None:
    rows = await conn.execute(text("PRAGMA index_list(nodes)"))
    indexes = rows.mappings().all()
    has_global_name_unique = False
    for index in indexes:
        if not index["unique"]:
            continue
        info = await conn.execute(text(f"PRAGMA index_info({index['name']})"))
        columns = [row._mapping["name"] for row in info]
        if columns == ["name"]:
            has_global_name_unique = True
            break
    if not has_global_name_unique:
        return

    await conn.execute(text("ALTER TABLE nodes RENAME TO nodes_legacy"))
    for index in indexes:
        index_name = index["name"]
        if not str(index_name).startswith("sqlite_autoindex"):
            await conn.execute(text(f"DROP INDEX {index_name}"))
    await conn.run_sync(Base.metadata.tables["nodes"].create)
    now = utcnow().isoformat()
    await conn.execute(
        text(
            """
            INSERT INTO nodes
                (id, task_id, name, type, server, port, listener_port, status, last_checked_at, created_at, updated_at)
            SELECT
                id,
                NULL,
                name,
                type,
                server,
                port,
                listener_port,
                COALESCE(status, 'unknown'),
                last_checked_at,
                COALESCE(created_at, :now),
                COALESCE(updated_at, :now)
            FROM nodes_legacy
            """
        ),
        {"now": now},
    )
    await conn.execute(text("DROP TABLE nodes_legacy"))


async def get_session() -> AsyncGenerator[AsyncSession, None]:
    if SessionLocal is None:
        init_engine()
    assert SessionLocal is not None
    async with SessionLocal() as session:
        yield session
