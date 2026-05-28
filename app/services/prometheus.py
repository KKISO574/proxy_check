from __future__ import annotations

from app.services.scoring import score_node

CONTENT_TYPE = "text/plain; version=0.0.4; charset=utf-8"


def _escape_label(value: object) -> str:
    return str(value).replace("\\", "\\\\").replace("\n", "\\n").replace('"', '\\"')


def _labels(values: dict[str, object]) -> str:
    return ",".join(f'{key}="{_escape_label(value)}"' for key, value in values.items())


def _metric_labels(node: object, *, metric: str | None = None, target: str | None = None) -> dict[str, object]:
    labels: dict[str, object] = {
        "node_id": getattr(node, "id", ""),
        "node_name": getattr(node, "name", ""),
        "task_id": getattr(node, "task_id", "") or "",
        "status": getattr(node, "status", ""),
    }
    if metric is not None:
        labels["metric"] = metric
    if target is not None:
        labels["target"] = target
    return labels


def build_prometheus_document(rows: list[dict[str, object]]) -> str:
    lines = [
        "# HELP proxy_check_node_score Computed node quality score.",
        "# TYPE proxy_check_node_score gauge",
        "# HELP proxy_check_node_score_confidence Share of score weight backed by current data.",
        "# TYPE proxy_check_node_score_confidence gauge",
        "# HELP proxy_check_node_availability Node availability as 1 for available and 0 otherwise.",
        "# TYPE proxy_check_node_availability gauge",
        "# HELP proxy_check_node_metric_latency_ms Latest metric latency in milliseconds.",
        "# TYPE proxy_check_node_metric_latency_ms gauge",
        "# HELP proxy_check_node_metric_value Latest metric value.",
        "# TYPE proxy_check_node_metric_value gauge",
    ]
    for row in rows:
        node = row["node"]
        metrics = row["metrics"]
        node_score = score_node(node, metrics)  # type: ignore[arg-type]
        base = _metric_labels(node)
        if node_score.score is not None:
            lines.append(f"proxy_check_node_score{{{_labels(base)}}} {node_score.score}")
        lines.append(
            f"proxy_check_node_score_confidence{{{_labels(base)}}} {node_score.confidence}"
        )
        availability = 1 if getattr(node, "status", "") == "available" else 0
        lines.append(f"proxy_check_node_availability{{{_labels(base)}}} {availability}")
        for summary in metrics.values():  # type: ignore[union-attr]
            metric = getattr(summary, "metric")
            target = getattr(summary, "target", "")
            metric_labels = _metric_labels(node, metric=metric, target=target)
            latency = getattr(summary, "latency_ms", None)
            if latency is not None:
                lines.append(
                    f"proxy_check_node_metric_latency_ms{{{_labels(metric_labels)}}} {float(latency)}"
                )
            value = getattr(summary, "value", None)
            if value is not None:
                lines.append(
                    f"proxy_check_node_metric_value{{{_labels(metric_labels)}}} {float(value)}"
                )
    return "\n".join(lines) + "\n"
