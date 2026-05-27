from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query, Request
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.schemas import (
    NodeDetail,
    NodeListItem,
    ProbePoint,
    RunResponse,
    StatsResponse,
    TaskCreateRequest,
    TaskImportResponse,
    TaskListItem,
    TaskUpdateRequest,
)
from app.services.config_import import ConfigImportError
from app.storage import repository
from app.storage.database import get_session
from app.storage.models import MonitorTask

router = APIRouter(prefix="/api")


def task_item(task: MonitorTask, node_count: int) -> TaskListItem:
    return TaskListItem(
        id=task.id,
        name=task.name,
        source_url=task.source_url,
        enabled=task.enabled,
        interval_seconds=task.interval_seconds,
        status=task.status,
        node_count=node_count,
        last_refresh_at=task.last_refresh_at,
        last_refresh_error=task.last_refresh_error,
        last_checked_at=task.last_checked_at,
        next_run_at=task.next_run_at,
    )


async def task_response(session: AsyncSession, task: MonitorTask) -> TaskListItem:
    counts = await repository.task_node_counts(session)
    return task_item(task, counts.get(task.id, 0))


@router.get("/tasks", response_model=list[TaskListItem])
async def list_tasks(session: AsyncSession = Depends(get_session)) -> list[TaskListItem]:
    tasks = await repository.list_tasks(session)
    counts = await repository.task_node_counts(session)
    return [task_item(task, counts.get(task.id, 0)) for task in tasks]


@router.post("/tasks", response_model=TaskImportResponse)
async def create_task(
    payload: TaskCreateRequest,
    request: Request,
    session: AsyncSession = Depends(get_session),
) -> TaskImportResponse:
    try:
        task, nodes = await request.app.state.config_import_service.create_task_from_url(
            session,
            name=payload.name,
            source_url=payload.source_url,
            interval_seconds=payload.interval_seconds,
            enabled=payload.enabled,
        )
    except ConfigImportError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return TaskImportResponse(task=await task_response(session, task), nodes=len(nodes))


@router.patch("/tasks/{task_id}", response_model=TaskImportResponse)
async def update_task(
    task_id: int,
    payload: TaskUpdateRequest,
    request: Request,
    session: AsyncSession = Depends(get_session),
) -> TaskImportResponse:
    task = await repository.get_task(session, task_id)
    if task is None:
        raise HTTPException(status_code=404, detail="task not found")
    values = payload.model_dump(exclude_unset=True)
    source_changed = "source_url" in values and values["source_url"] != task.source_url
    await repository.update_task(session, task, **values)
    nodes = await repository.list_nodes(session, task_id=task.id)
    if source_changed:
        try:
            task, nodes = await request.app.state.config_import_service.refresh_task(session, task.id)
        except ConfigImportError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
    return TaskImportResponse(task=await task_response(session, task), nodes=len(nodes))


@router.delete("/tasks/{task_id}", status_code=204)
async def delete_task(task_id: int, session: AsyncSession = Depends(get_session)) -> None:
    task = await repository.get_task(session, task_id)
    if task is None:
        raise HTTPException(status_code=404, detail="task not found")
    await repository.delete_task(session, task)


@router.post("/tasks/{task_id}/refresh", response_model=TaskImportResponse)
async def refresh_task(
    task_id: int,
    request: Request,
    session: AsyncSession = Depends(get_session),
) -> TaskImportResponse:
    try:
        task, nodes = await request.app.state.config_import_service.refresh_task(session, task_id)
    except ConfigImportError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return TaskImportResponse(task=await task_response(session, task), nodes=len(nodes))


@router.post("/tasks/{task_id}/run", response_model=RunResponse)
async def run_task(task_id: int, request: Request) -> RunResponse:
    summary = await request.app.state.probe_service.run_task(task_id)
    return RunResponse(nodes=summary.nodes, results=summary.results, errors=summary.errors)


@router.get("/nodes", response_model=list[NodeListItem])
async def list_nodes(
    task_id: int | None = Query(default=None),
    session: AsyncSession = Depends(get_session),
) -> list[NodeListItem]:
    rows = await repository.nodes_with_latest_metrics(session, task_id=task_id)
    output: list[NodeListItem] = []
    for item in rows:
        node = item["node"]
        delay = item["delay"]
        tcping = item["tcping"]
        output.append(
            NodeListItem(
                id=node.id,
                task_id=node.task_id,
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
        task_id=node.task_id,
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
async def stats(
    task_id: int | None = Query(default=None),
    session: AsyncSession = Depends(get_session),
) -> StatsResponse:
    return StatsResponse.model_validate(await repository.stats(session, task_id=task_id))


@router.post("/tests/run", response_model=RunResponse)
async def run_tests(request: Request) -> RunResponse:
    service = request.app.state.probe_service
    summary = await service.run_once()
    return RunResponse(nodes=summary.nodes, results=summary.results, errors=summary.errors)
