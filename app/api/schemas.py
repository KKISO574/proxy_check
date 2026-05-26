from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel


class NodeListItem(BaseModel):
    id: int
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


NodeDetail.model_rebuild()
