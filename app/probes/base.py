from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from app.core.config import Settings
from app.probes.mihomo import MihomoClient
from app.storage.models import Node


@dataclass(frozen=True)
class ProbeOutcome:
    metric: str
    target: str = ""
    latency_ms: float | None = None
    value: float | None = None
    data: str | None = None
    success: bool = False
    error: str | None = None


@dataclass(frozen=True)
class ProbeContext:
    node: Node
    settings: Settings
    client: MihomoClient | None
    mihomo_error: str | None = None


class Prober(Protocol):
    metric: str
    interval_seconds: int

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        """Run one probe dimension for a node."""
