from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

from app.storage.models import Node


@dataclass(frozen=True)
class ScoreComponent:
    weight: float
    score: float
    contribution: float
    value: float | None
    status: str


@dataclass(frozen=True)
class NodeScore:
    score: float | None
    confidence: float
    breakdown: dict[str, ScoreComponent]


TOTAL_WEIGHT = 100.0


def _clamp(value: float, low: float = 0.0, high: float = 100.0) -> float:
    return max(low, min(high, value))


def _metric_value(summary: object | None) -> float | None:
    if summary is None:
        return None
    latency = getattr(summary, "latency_ms", None)
    if latency is not None:
        return float(latency)
    value = getattr(summary, "value", None)
    if value is not None:
        return float(value)
    return None


def _latency_score(value: float, *, excellent: float, poor: float) -> float:
    if value <= excellent:
        return 100.0
    if value >= poor:
        return 0.0
    return _clamp(((poor - value) / (poor - excellent)) * 100)


def _component(
    *,
    weight: float,
    score: float,
    value: float | None,
    status: str,
) -> ScoreComponent:
    return ScoreComponent(
        weight=weight,
        score=round(score, 2),
        contribution=round((score * weight) / TOTAL_WEIGHT, 2),
        value=value,
        status=status,
    )


def _summary_success(summary: object | None) -> bool | None:
    if summary is None:
        return None
    return bool(getattr(summary, "success", False))


def score_node(node: Node, metrics: Mapping[str, object]) -> NodeScore:
    breakdown: dict[str, ScoreComponent] = {}

    status_score = {
        "available": 100.0,
        "unknown": 50.0,
        "down": 0.0,
        "removed": 0.0,
    }.get(node.status, 50.0)
    breakdown["status"] = _component(
        weight=10.0,
        score=status_score,
        value=None,
        status=node.status,
    )

    delay = metrics.get("delay")
    if delay is not None:
        value = _metric_value(delay)
        score = 0.0 if not _summary_success(delay) or value is None else _latency_score(value, excellent=100, poor=1000)
        breakdown["delay"] = _component(
            weight=35.0,
            score=score,
            value=value,
            status="ok" if _summary_success(delay) else "failed",
        )

    packet_loss = metrics.get("packet_loss")
    if packet_loss is not None:
        value = _metric_value(packet_loss)
        score = 0.0 if not _summary_success(packet_loss) or value is None else _clamp(100 - (value * 4))
        breakdown["packet_loss"] = _component(
            weight=25.0,
            score=score,
            value=value,
            status="ok" if _summary_success(packet_loss) else "failed",
        )

    jitter = metrics.get("jitter")
    if jitter is not None:
        value = _metric_value(jitter)
        score = 0.0 if not _summary_success(jitter) or value is None else _latency_score(value, excellent=20, poor=200)
        breakdown["jitter"] = _component(
            weight=15.0,
            score=score,
            value=value,
            status="ok" if _summary_success(jitter) else "failed",
        )

    transport_scores: list[float] = []
    transport_values: list[float] = []
    transport_failed = False
    for metric in ("tcping", "http_rtt", "tls_handshake"):
        summary = metrics.get(metric)
        if summary is None:
            continue
        value = _metric_value(summary)
        if not _summary_success(summary) or value is None:
            transport_failed = True
            transport_scores.append(0.0)
            continue
        transport_values.append(value)
        transport_scores.append(_latency_score(value, excellent=100, poor=1500))
    if transport_scores:
        avg_score = sum(transport_scores) / len(transport_scores)
        avg_value = sum(transport_values) / len(transport_values) if transport_values else None
        breakdown["transport"] = _component(
            weight=15.0,
            score=avg_score,
            value=avg_value,
            status="failed" if transport_failed and not transport_values else "ok",
        )

    available_weight = sum(item.weight for item in breakdown.values())
    if available_weight <= 0:
        return NodeScore(score=None, confidence=0.0, breakdown={})
    weighted_score = sum(item.score * item.weight for item in breakdown.values()) / available_weight
    return NodeScore(
        score=round(weighted_score, 2),
        confidence=round(available_weight / TOTAL_WEIGHT, 2),
        breakdown=breakdown,
    )
