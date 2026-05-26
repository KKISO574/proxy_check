from __future__ import annotations

from datetime import datetime, timedelta, timezone

from sqlalchemy import Select, and_, delete, func, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import aliased

from app.core.clash_config import ClashNode
from app.storage.models import Node, ProbeResult, utcnow


async def upsert_nodes(
    session: AsyncSession,
    nodes: list[ClashNode],
    listener_ports: dict[str, int],
) -> list[Node]:
    existing = {
        node.name: node
        for node in (await session.execute(select(Node))).scalars().all()
    }
    output: list[Node] = []
    names = {node.name for node in nodes}

    for item in nodes:
        node = existing.get(item.name)
        if node is None:
            node = Node(name=item.name)
            session.add(node)
        node.type = item.type
        node.server = item.server
        node.port = item.port
        node.listener_port = listener_ports.get(item.name)
        node.updated_at = utcnow()
        output.append(node)

    for name, node in existing.items():
        if name not in names:
            node.status = "removed"
            node.updated_at = utcnow()

    await session.commit()
    for node in output:
        await session.refresh(node)
    return output


async def list_nodes(session: AsyncSession) -> list[Node]:
    rows = await session.execute(select(Node).order_by(Node.name.asc()))
    return list(rows.scalars().all())


async def get_node(session: AsyncSession, node_id: int) -> Node | None:
    return await session.get(Node, node_id)


async def add_probe_result(
    session: AsyncSession,
    node: Node,
    *,
    metric: str,
    target: str,
    latency_ms: float | None,
    success: bool,
    error: str | None,
    at: datetime | None = None,
) -> ProbeResult:
    result = ProbeResult(
        node_id=node.id,
        metric=metric,
        target=target,
        latency_ms=latency_ms,
        success=success,
        error=error,
        created_at=at or utcnow(),
    )
    session.add(result)
    return result


async def save_probe_batch(
    session: AsyncSession,
    node: Node,
    results: list[dict[str, object]],
    *,
    at: datetime | None = None,
) -> None:
    timestamp = at or utcnow()
    success_count = 0
    for item in results:
        success = bool(item["success"])
        if success:
            success_count += 1
        await add_probe_result(
            session,
            node,
            metric=str(item["metric"]),
            target=str(item.get("target") or ""),
            latency_ms=item.get("latency_ms"),  # type: ignore[arg-type]
            success=success,
            error=item.get("error"),  # type: ignore[arg-type]
            at=timestamp,
        )
    node.status = "available" if success_count > 0 else "down"
    node.last_checked_at = timestamp
    node.updated_at = timestamp


async def latest_result_subquery(metric: str) -> Select[tuple[int, int]]:
    return (
        select(ProbeResult.node_id, func.max(ProbeResult.id).label("result_id"))
        .where(ProbeResult.metric == metric)
        .group_by(ProbeResult.node_id)
    )


async def nodes_with_latest_metrics(session: AsyncSession) -> list[dict[str, object]]:
    delay_latest = (await latest_result_subquery("delay")).subquery()
    tcp_latest = (await latest_result_subquery("tcping")).subquery()
    delay_result = aliased(ProbeResult)
    tcp_result = aliased(ProbeResult)

    stmt = (
        select(Node, delay_result, tcp_result)
        .outerjoin(delay_latest, delay_latest.c.node_id == Node.id)
        .outerjoin(
            delay_result,
            and_(
                delay_result.node_id == Node.id,
                delay_result.metric == "delay",
                delay_result.id == delay_latest.c.result_id,
            ),
        )
        .outerjoin(tcp_latest, tcp_latest.c.node_id == Node.id)
        .outerjoin(
            tcp_result,
            and_(
                tcp_result.node_id == Node.id,
                tcp_result.metric == "tcping",
                tcp_result.id == tcp_latest.c.result_id,
            ),
        )
        .order_by(Node.name.asc())
    )
    rows = (await session.execute(stmt)).all()
    return [
        {
            "node": node,
            "delay": delay,
            "tcping": tcping,
        }
        for node, delay, tcping in rows
    ]


async def node_history(
    session: AsyncSession,
    node_id: int,
    *,
    metric: str,
    range_name: str,
) -> list[ProbeResult]:
    ranges = {
        "1h": timedelta(hours=1),
        "6h": timedelta(hours=6),
        "24h": timedelta(hours=24),
        "7d": timedelta(days=7),
        "30d": timedelta(days=30),
    }
    since = datetime.now(timezone.utc) - ranges.get(range_name, ranges["24h"])
    rows = await session.execute(
        select(ProbeResult)
        .where(
            ProbeResult.node_id == node_id,
            ProbeResult.metric == metric,
            ProbeResult.created_at >= since,
        )
        .order_by(ProbeResult.created_at.asc())
    )
    return list(rows.scalars().all())


async def recent_errors(session: AsyncSession, node_id: int, limit: int = 20) -> list[ProbeResult]:
    rows = await session.execute(
        select(ProbeResult)
        .where(ProbeResult.node_id == node_id, ProbeResult.success.is_(False))
        .order_by(ProbeResult.created_at.desc())
        .limit(limit)
    )
    return list(rows.scalars().all())


async def cleanup_old_results(session: AsyncSession, retention_days: int) -> int:
    cutoff = datetime.now(timezone.utc) - timedelta(days=retention_days)
    result = await session.execute(delete(ProbeResult).where(ProbeResult.created_at < cutoff))
    await session.commit()
    return int(result.rowcount or 0)


async def stats(session: AsyncSession) -> dict[str, object]:
    rows = await session.execute(select(Node))
    nodes = list(rows.scalars().all())
    total = len(nodes)
    available = len([node for node in nodes if node.status == "available"])
    down = len([node for node in nodes if node.status == "down"])

    latest = await nodes_with_latest_metrics(session)
    latencies = [
        item["delay"].latency_ms
        for item in latest
        if item["delay"] is not None and item["delay"].success and item["delay"].latency_ms is not None
    ]
    avg_latency = sum(latencies) / len(latencies) if latencies else None
    return {
        "total_nodes": total,
        "available_nodes": available,
        "down_nodes": down,
        "unknown_nodes": total - available - down,
        "average_delay_ms": avg_latency,
    }
