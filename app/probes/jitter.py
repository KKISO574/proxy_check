"""Jitter probe derived from recent ``delay`` samples.

Reads the last N successful ``delay`` measurements for the node from the
database and reports the population standard deviation as ``jitter``. When
fewer than 2 samples exist the prober returns an empty outcome list (no
record is written) — accumulating failures every cycle would pollute
``probe_results`` while delay history bootstraps. The driver tolerates an
empty list naturally.
"""

from __future__ import annotations

import json
import math

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.probes.base import ProbeContext, ProbeOutcome
from app.storage.models import ProbeResult


class JitterProber:
    metric = "jitter"
    interval_seconds = 60

    def __init__(
        self,
        session_factory: async_sessionmaker[AsyncSession],
        *,
        sample_size: int = 20,
    ) -> None:
        self.session_factory = session_factory
        self.sample_size = sample_size

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        async with self.session_factory() as session:
            rows = await session.execute(
                select(ProbeResult.latency_ms)
                .where(
                    ProbeResult.node_id == context.node.id,
                    ProbeResult.metric == "delay",
                    ProbeResult.success.is_(True),
                    ProbeResult.latency_ms.is_not(None),
                )
                .order_by(ProbeResult.created_at.desc(), ProbeResult.id.desc())
                .limit(self.sample_size)
            )
            samples = [float(value) for value in rows.scalars().all()]
        if len(samples) < 2:
            # Insufficient delay history to derive jitter — emit no record.
            # Writing a synthetic failure here would pollute probe_results
            # every cycle until enough delay samples accumulate; the upstream
            # _probe_node loop tolerates an empty outcome list.
            return []
        mean = sum(samples) / len(samples)
        variance = sum((sample - mean) ** 2 for sample in samples) / len(samples)
        jitter = math.sqrt(variance)
        return [
            ProbeOutcome(
                metric=self.metric,
                target="delay:last_samples",
                value=jitter,
                success=True,
                data=json.dumps({"samples": len(samples)}),
            )
        ]
