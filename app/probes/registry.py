from __future__ import annotations

from collections.abc import Iterable
from datetime import datetime, timedelta, timezone

from app.probes.base import Prober


class ProbeRegistry:
    def __init__(self, probers: Iterable[Prober] | None = None) -> None:
        self._probers: list[Prober] = list(probers or [])

    def register(self, prober: Prober) -> None:
        self._probers.append(prober)

    def enabled(self, dimensions: list[str] | None = None) -> list[Prober]:
        if not dimensions:
            return list(self._probers)
        selected = set(dimensions)
        return [prober for prober in self._probers if prober.metric in selected]

    def metrics(self, dimensions: list[str] | None = None) -> list[str]:
        return [prober.metric for prober in self.enabled(dimensions)]

    def due(
        self,
        dimensions: list[str] | None,
        last_seen: dict[str, datetime],
        *,
        now: datetime | None = None,
    ) -> list[Prober]:
        """Return enabled probers whose `interval_seconds` has elapsed since
        the last successful run.

        Probers with no recorded success are always due. The comparison
        applies a small slack so a probe scheduled every 60s does not get
        skipped when the previous run finished 59.5s ago.
        """
        current = now or datetime.now(timezone.utc)
        # Allow up to 5% slack (capped at 5s) so probes scheduled at the
        # task's interval don't slip an entire cycle due to scheduler jitter.
        result: list[Prober] = []
        for prober in self.enabled(dimensions):
            interval = max(int(getattr(prober, "interval_seconds", 0) or 0), 0)
            if interval <= 0:
                result.append(prober)
                continue
            previous = last_seen.get(prober.metric)
            if previous is None:
                result.append(prober)
                continue
            if previous.tzinfo is None:
                previous = previous.replace(tzinfo=timezone.utc)
            slack = min(max(interval * 0.05, 0.0), 5.0)
            elapsed = (current - previous).total_seconds()
            if elapsed + slack >= interval:
                result.append(prober)
        return result
