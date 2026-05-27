from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.probes.mihomo import MihomoUnavailable
from app.storage.models import Node


def allocate_listener_ports(
    *,
    desired_names: list[str],
    existing_assignments: dict[str, int],
    occupied: set[int],
    port_start: int,
    port_max: int,
) -> dict[str, int]:
    """Assign a unique listener port to each desired node name.

    Existing assignments are reused when their port is still inside
    ``[port_start, port_max]``; otherwise a fresh port is picked from the
    lowest free slot above ``port_start``. Ports already in use by other
    tasks are excluded via ``occupied``. Raises :class:`MihomoUnavailable`
    when no free port is available within the configured range.
    """
    assignments: dict[str, int] = {}
    used: set[int] = set(occupied)

    # First pass: keep stable port assignments for nodes we have seen before.
    for name in desired_names:
        port = existing_assignments.get(name)
        if port is not None and port_start <= port <= port_max:
            assignments[name] = port
            used.add(port)

    # Second pass: assign new ports for the remainder, scanning upwards.
    candidate = port_start
    for name in desired_names:
        if name in assignments:
            continue
        while candidate in used:
            candidate += 1
        if candidate > port_max:
            raise MihomoUnavailable(
                f"listener port range exhausted (start={port_start}, max={port_max})"
            )
        assignments[name] = candidate
        used.add(candidate)
        candidate += 1

    return assignments


async def allocate_for_task(
    session: AsyncSession,
    *,
    task_id: int | None,
    desired_names: list[str],
    port_start: int,
    port_max: int,
) -> dict[str, int]:
    """Database-aware variant: load existing assignments + global occupancy.

    ``occupied`` is collected from every node in the database except those
    that belong to ``task_id`` (their ports are subject to reassignment via
    ``existing_assignments``). Nodes flagged ``status='removed'`` still hold
    their port slot until they are deleted, which keeps allocations stable
    while a config refresh is in progress.
    """
    existing_rows = (
        await session.execute(
            select(Node.name, Node.listener_port).where(Node.task_id == task_id)
        )
    ).all()
    existing_assignments: dict[str, int] = {
        name: port for name, port in existing_rows if port is not None
    }

    occupied_rows = (
        await session.execute(
            select(Node.listener_port).where(
                Node.listener_port.is_not(None),
                Node.task_id != task_id if task_id is not None else Node.task_id.is_not(None),
            )
        )
    ).all()
    occupied: set[int] = {int(port) for (port,) in occupied_rows if port is not None}

    return allocate_listener_ports(
        desired_names=desired_names,
        existing_assignments=existing_assignments,
        occupied=occupied,
        port_start=port_start,
        port_max=port_max,
    )
