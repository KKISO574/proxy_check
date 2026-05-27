from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel


class NodeListItem(BaseModel):
    id: int
    task_id: int | None = None
    name: str
    type: str | None
    server: str | None
    port: int | None
    listener_port: int | None
    status: str
    latest_delay_ms: float | None
    latest_tcping_ms: float | None
    latest_tcping_target: str | None
    last_checked_at: datetime | None


class NodeDetail(NodeListItem):
    recent_errors: list["ProbePoint"]


class ProbePoint(BaseModel):
    created_at: datetime
    metric: str
    target: str
    latency_ms: float | None
    success: bool
    error: str | None


class StatsResponse(BaseModel):
    total_nodes: int
    available_nodes: int
    down_nodes: int
    unknown_nodes: int
    average_delay_ms: float | None


class RunResponse(BaseModel):
    nodes: int
    results: int
    errors: int


class TaskListItem(BaseModel):
    id: int
    name: str
    source_url: str
    enabled: bool
    interval_seconds: int
    status: str
    node_count: int
    last_refresh_at: datetime | None
    last_refresh_error: str | None
    last_checked_at: datetime | None
    next_run_at: datetime | None


class TaskCreateRequest(BaseModel):
    name: str
    source_url: str
    interval_seconds: int = 60
    enabled: bool = True


class TaskUpdateRequest(BaseModel):
    name: str | None = None
    source_url: str | None = None
    interval_seconds: int | None = None
    enabled: bool | None = None


class TaskImportResponse(BaseModel):
    task: TaskListItem
    nodes: int


NodeDetail.model_rebuild()
