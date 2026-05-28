from __future__ import annotations

from datetime import datetime, timedelta, timezone

from sqlalchemy import Select, and_, delete, func, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import aliased

from app.core.clash_config import ClashNode
from app.storage.models import MonitorTask, Node, NodeMeta, ProbeResult, utcnow


class MetricSummary:
    def __init__(
        self,
        *,
        metric: str,
        target: str,
        latency_ms: float | None,
        value: float | None,
        data: str | None,
        success: bool,
        error: str | None,
        created_at: datetime,
    ) -> None:
        self.metric = metric
        self.target = target
        self.latency_ms = latency_ms
        self.value = value
        self.data = data
        self.success = success
        self.error = error
        self.created_at = created_at


def metric_summary(result: ProbeResult) -> MetricSummary:
    return MetricSummary(
        metric=result.metric,
        target=result.target,
        latency_ms=result.latency_ms,
        value=result.value,
        data=result.data,
        success=result.success,
        error=result.error,
        created_at=result.created_at,
    )


async def create_task(
    session: AsyncSession,
    *,
    name: str,
    source_url: str,
    config_path: str,
    interval_seconds: int,
    enabled: bool = True,
) -> MonitorTask:
    now = utcnow()
    task = MonitorTask(
        name=name,
        source_url=source_url,
        config_path=config_path,
        interval_seconds=interval_seconds,
        enabled=enabled,
        status="unknown",
        created_at=now,
        updated_at=now,
    )
    session.add(task)
    await session.commit()
    await session.refresh(task)
    return task


async def get_task(session: AsyncSession, task_id: int) -> MonitorTask | None:
    return await session.get(MonitorTask, task_id)


async def list_tasks(session: AsyncSession) -> list[MonitorTask]:
    rows = await session.execute(select(MonitorTask).order_by(MonitorTask.id.asc()))
    return list(rows.scalars().all())


async def update_task(
    session: AsyncSession,
    task: MonitorTask,
    **values: object,
) -> MonitorTask:
    for key, value in values.items():
        setattr(task, key, value)
    task.updated_at = utcnow()
    await session.commit()
    await session.refresh(task)
    return task


async def delete_task(session: AsyncSession, task: MonitorTask) -> None:
    await session.delete(task)
    await session.commit()


async def task_node_counts(session: AsyncSession) -> dict[int, int]:
    rows = await session.execute(
        select(Node.task_id, func.count(Node.id))
        .where(Node.task_id.is_not(None), Node.status != "removed")
        .group_by(Node.task_id)
    )
    return {int(task_id): int(count) for task_id, count in rows.all() if task_id is not None}


async def upsert_nodes(
    session: AsyncSession,
    nodes: list[ClashNode],
    listener_ports: dict[str, int],
    *,
    task_id: int | None = None,
) -> list[Node]:
    existing = {
        node.name: node
        for node in (
            await session.execute(select(Node).where(Node.task_id == task_id))
        ).scalars().all()
    }
    output: list[Node] = []
    names = {node.name for node in nodes}

    for item in nodes:
        node = existing.get(item.name)
        if node is None:
            node = Node(name=item.name, task_id=task_id)
            session.add(node)
        node.task_id = task_id
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


async def list_nodes(session: AsyncSession, *, task_id: int | None = None) -> list[Node]:
    stmt = select(Node).order_by(Node.name.asc())
    if task_id is not None:
        stmt = stmt.where(Node.task_id == task_id)
    rows = await session.execute(stmt)
    return list(rows.scalars().all())


async def get_node(session: AsyncSession, node_id: int) -> Node | None:
    return await session.get(Node, node_id)


async def last_metric_timestamps(
    session: AsyncSession, node_id: int
) -> dict[str, datetime]:
    rows = await session.execute(
        select(ProbeResult.metric, func.max(ProbeResult.created_at))
        .where(
            ProbeResult.node_id == node_id,
            ProbeResult.success.is_(True),
        )
        .group_by(ProbeResult.metric)
    )
    return {metric: ts for metric, ts in rows.all() if metric is not None and ts is not None}


async def add_probe_result(
    session: AsyncSession,
    node: Node,
    *,
    metric: str,
    target: str,
    latency_ms: float | None,
    value: float | None = None,
    data: str | None = None,
    success: bool,
    error: str | None,
    at: datetime | None = None,
) -> ProbeResult:
    result = ProbeResult(
        node_id=node.id,
        metric=metric,
        target=target,
        latency_ms=latency_ms,
        value=value,
        data=data,
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
            value=item.get("value"),  # type: ignore[arg-type]
            data=item.get("data"),  # type: ignore[arg-type]
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


async def nodes_with_latest_metrics(
    session: AsyncSession,
    *,
    task_id: int | None = None,
    metrics: list[str] | None = None,
) -> list[dict[str, object]]:
    metric_names = metrics
    latest = (
        select(ProbeResult.node_id, ProbeResult.metric, func.max(ProbeResult.id).label("result_id"))
        .group_by(ProbeResult.node_id, ProbeResult.metric)
        .subquery()
    )
    if metric_names is not None:
        latest = (
            select(ProbeResult.node_id, ProbeResult.metric, func.max(ProbeResult.id).label("result_id"))
            .where(ProbeResult.metric.in_(metric_names))
            .group_by(ProbeResult.node_id, ProbeResult.metric)
            .subquery()
        )
    result_alias = aliased(ProbeResult)
    stmt = select(Node, result_alias).outerjoin(
        latest,
        latest.c.node_id == Node.id,
    ).outerjoin(
        result_alias,
        and_(
            result_alias.node_id == Node.id,
            result_alias.metric == latest.c.metric,
            result_alias.id == latest.c.result_id,
        )
    )
    if task_id is not None:
        stmt = stmt.where(Node.task_id == task_id)
    stmt = stmt.order_by(Node.name.asc())
    rows = (await session.execute(stmt)).all()
    by_node: dict[int, dict[str, object]] = {}
    for node, result in rows:
        item = by_node.setdefault(
            node.id,
            {
                "node": node,
                "metrics": {},
            },
        )
        if result is None:
            continue
        summary = metric_summary(result)
        item["metrics"][result.metric] = summary  # type: ignore[index]
    return list(by_node.values())


async def get_node_meta(session: AsyncSession, node_id: int) -> NodeMeta | None:
    rows = await session.execute(select(NodeMeta).where(NodeMeta.node_id == node_id))
    return rows.scalar_one_or_none()


async def upsert_node_meta(
    session: AsyncSession,
    node: Node,
    **values: str | None,
) -> NodeMeta:
    meta = await get_node_meta(session, node.id)
    if meta is None:
        meta = NodeMeta(node_id=node.id)
        session.add(meta)
    for key, value in values.items():
        setattr(meta, key, value)
    meta.updated_at = utcnow()
    await session.flush()
    return meta


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


async def stats(session: AsyncSession, *, task_id: int | None = None) -> dict[str, object]:
    stmt = select(Node)
    if task_id is not None:
        stmt = stmt.where(Node.task_id == task_id)
    rows = await session.execute(stmt)
    nodes = list(rows.scalars().all())
    total = len(nodes)
    available = len([node for node in nodes if node.status == "available"])
    down = len([node for node in nodes if node.status == "down"])

    latest = await nodes_with_latest_metrics(session, task_id=task_id)
    latencies: list[float] = []
    for item in latest:
        delay = item["metrics"].get("delay")  # type: ignore[union-attr]
        if delay is not None and delay.success and delay.latency_ms is not None:
            latencies.append(delay.latency_ms)
    avg_latency = sum(latencies) / len(latencies) if latencies else None
    return {
        "total_nodes": total,
        "available_nodes": available,
        "down_nodes": down,
        "unknown_nodes": total - available - down,
        "average_delay_ms": avg_latency,
    }
