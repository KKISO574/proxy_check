from __future__ import annotations

import pytest

from app.services.scoring import score_node
from app.storage.models import Node
from app.storage.repository import MetricSummary
from app.storage.models import utcnow


def summary(
    metric: str,
    *,
    latency_ms: float | None = None,
    value: float | None = None,
    success: bool = True,
) -> MetricSummary:
    return MetricSummary(
        metric=metric,
        target="target",
        latency_ms=latency_ms,
        value=value,
        data=None,
        success=success,
        error=None if success else "failed",
        created_at=utcnow(),
    )


def test_score_node_uses_weighted_metrics_and_confidence() -> None:
    node = Node(name="node-a", status="available")
    metrics = {
        "delay": summary("delay", latency_ms=100, value=100),
        "packet_loss": summary("packet_loss", value=0),
        "jitter": summary("jitter", value=20),
        "tcping": summary("tcping", latency_ms=100, value=100),
        "http_rtt": summary("http_rtt", latency_ms=100, value=100),
        "tls_handshake": summary("tls_handshake", latency_ms=100, value=100),
    }

    score = score_node(node, metrics)

    assert score.score == pytest.approx(100.0)
    assert score.confidence == pytest.approx(1.0)
    assert set(score.breakdown) == {"delay", "packet_loss", "jitter", "transport", "status"}
    assert score.breakdown["transport"].weight == 15


def test_score_node_keeps_unknown_metrics_out_of_confidence() -> None:
    node = Node(name="node-a", status="unknown")

    score = score_node(node, {})

    assert score.score == pytest.approx(50.0)
    assert score.confidence == pytest.approx(0.1)
    assert list(score.breakdown) == ["status"]


def test_failed_metric_counts_as_zero_with_available_confidence() -> None:
    node = Node(name="node-a", status="available")
    metrics = {"packet_loss": summary("packet_loss", success=False)}

    score = score_node(node, metrics)

    assert score.breakdown["packet_loss"].score == 0
    assert score.confidence == pytest.approx(0.35)
