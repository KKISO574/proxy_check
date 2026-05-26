from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query, Request
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.schemas import NodeDetail, NodeListItem, ProbePoint, RunResponse, StatsResponse
from app.storage import repository
from app.storage.database import get_session

router = APIRouter(prefix="/api")


@router.get("/nodes", response_model=list[NodeListItem])
async def list_nodes(session: AsyncSession = Depends(get_session)) -> list[NodeListItem]:
    rows = await repository.nodes_with_latest_metrics(session)
    output: list[NodeListItem] = []
    for item in rows:
        node = item["node"]
        delay = item["delay"]
        tcping = item["tcping"]
        output.append(
            NodeListItem(
                id=node.id,
                name=node.name,
                type=node.type,
                server=node.server,
                port=node.port,
                listener_port=node.listener_port,
                status=node.status,
                latest_delay_ms=delay.latency_ms if delay is not None else None,
                latest_tcping_ms=tcping.latency_ms if tcping is not None else None,
                latest_tcping_target=tcping.target if tcping is not None else None,
                last_checked_at=node.last_checked_at,
            )
        )
    return output


@router.get("/nodes/{node_id}", response_model=NodeDetail)
async def node_detail(
    node_id: int,
    session: AsyncSession = Depends(get_session),
) -> NodeDetail:
    node = await repository.get_node(session, node_id)
    if node is None:
        raise HTTPException(status_code=404, detail="node not found")
    latest = await repository.nodes_with_latest_metrics(session)
    latest_for_node = next((item for item in latest if item["node"].id == node_id), None)
    delay = latest_for_node["delay"] if latest_for_node is not None else None
    tcping = latest_for_node["tcping"] if latest_for_node is not None else None
    errors = await repository.recent_errors(session, node_id)
    return NodeDetail(
        id=node.id,
        name=node.name,
        type=node.type,
        server=node.server,
        port=node.port,
        listener_port=node.listener_port,
        status=node.status,
        latest_delay_ms=delay.latency_ms if delay is not None else None,
        latest_tcping_ms=tcping.latency_ms if tcping is not None else None,
        latest_tcping_target=tcping.target if tcping is not None else None,
        last_checked_at=node.last_checked_at,
        recent_errors=[
            ProbePoint(
                created_at=item.created_at,
                metric=item.metric,
                target=item.target,
                latency_ms=item.latency_ms,
                success=item.success,
                error=item.error,
            )
            for item in errors
        ],
    )


@router.get("/nodes/{node_id}/history", response_model=list[ProbePoint])
async def node_history(
    node_id: int,
    metric: str = Query(pattern="^(delay|tcping)$"),
    range: str = Query(default="24h", pattern="^(1h|6h|24h|7d|30d)$"),
    session: AsyncSession = Depends(get_session),
) -> list[ProbePoint]:
    node = await repository.get_node(session, node_id)
    if node is None:
        raise HTTPException(status_code=404, detail="node not found")
    rows = await repository.node_history(session, node_id, metric=metric, range_name=range)
    return [
        ProbePoint(
            created_at=item.created_at,
            metric=item.metric,
            target=item.target,
            latency_ms=item.latency_ms,
            success=item.success,
            error=item.error,
        )
        for item in rows
    ]


@router.get("/stats", response_model=StatsResponse)
async def stats(session: AsyncSession = Depends(get_session)) -> StatsResponse:
    return StatsResponse.model_validate(await repository.stats(session))


@router.post("/tests/run", response_model=RunResponse)
async def run_tests(request: Request) -> RunResponse:
    service = request.app.state.probe_service
    summary = await service.run_once()
    return RunResponse(nodes=summary.nodes, results=summary.results, errors=summary.errors)
