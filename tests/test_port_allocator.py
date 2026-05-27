from __future__ import annotations

import pytest
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from app.probes.mihomo import MihomoUnavailable
from app.services.port_allocator import allocate_for_task, allocate_listener_ports
from app.storage.models import Base, MonitorTask, Node


def test_allocate_picks_lowest_free_slot_above_start():
    result = allocate_listener_ports(
        desired_names=["a", "b", "c"],
        existing_assignments={},
        occupied=set(),
        port_start=20000,
        port_max=65000,
    )
    assert result == {"a": 20000, "b": 20001, "c": 20002}


def test_allocate_keeps_existing_assignments_stable():
    result = allocate_listener_ports(
        desired_names=["a", "b", "c"],
        existing_assignments={"a": 20005, "c": 20003},
        occupied=set(),
        port_start=20000,
        port_max=65000,
    )
    assert result["a"] == 20005
    assert result["c"] == 20003
    # b takes the lowest free slot, skipping 20003 and 20005 which are reused.
    assert result["b"] == 20000


def test_allocate_skips_ports_occupied_by_other_tasks():
    result = allocate_listener_ports(
        desired_names=["a", "b"],
        existing_assignments={},
        occupied={20000, 20001, 20003},
        port_start=20000,
        port_max=65000,
    )
    assert result == {"a": 20002, "b": 20004}


def test_allocate_raises_when_range_exhausted():
    with pytest.raises(MihomoUnavailable, match="listener port range"):
        allocate_listener_ports(
            desired_names=["a", "b", "c"],
            existing_assignments={},
            occupied=set(),
            port_start=20000,
            port_max=20001,
        )


def test_allocate_drops_existing_outside_range():
    result = allocate_listener_ports(
        desired_names=["a"],
        existing_assignments={"a": 70000},  # out-of-range stale value
        occupied=set(),
        port_start=20000,
        port_max=65000,
    )
    assert result == {"a": 20000}


@pytest.mark.asyncio
async def test_allocate_for_task_does_not_collide_across_tasks():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    try:
        async with session_factory() as session:
            task_a = MonitorTask(name="A", source_url="https://a/x.yaml", config_path="/tmp/a.yaml")
            task_b = MonitorTask(name="B", source_url="https://b/x.yaml", config_path="/tmp/b.yaml")
            session.add_all([task_a, task_b])
            await session.flush()
            # Pre-assign three listener ports for task A.
            session.add_all(
                [
                    Node(task_id=task_a.id, name="a-1", listener_port=20000, status="available"),
                    Node(task_id=task_a.id, name="a-2", listener_port=20001, status="available"),
                    Node(task_id=task_a.id, name="a-3", listener_port=20002, status="available"),
                ]
            )
            await session.commit()

            ports_b = await allocate_for_task(
                session,
                task_id=task_b.id,
                desired_names=["b-1", "b-2"],
                port_start=20000,
                port_max=65000,
            )
            assert set(ports_b.values()) == {20003, 20004}
            assert set(ports_b.values()).isdisjoint({20000, 20001, 20002})
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_allocate_for_task_reuses_existing_within_same_task():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    try:
        async with session_factory() as session:
            task = MonitorTask(name="A", source_url="https://a/x.yaml", config_path="/tmp/a.yaml")
            session.add(task)
            await session.flush()
            session.add_all(
                [
                    Node(task_id=task.id, name="alpha", listener_port=20007, status="available"),
                    Node(task_id=task.id, name="beta", listener_port=20009, status="available"),
                ]
            )
            await session.commit()

            ports = await allocate_for_task(
                session,
                task_id=task.id,
                desired_names=["alpha", "beta", "gamma"],
                port_start=20000,
                port_max=65000,
            )
            assert ports["alpha"] == 20007
            assert ports["beta"] == 20009
            # gamma takes the lowest free slot (20000) since 20007/20009 are reused.
            assert ports["gamma"] == 20000
    finally:
        await engine.dispose()
