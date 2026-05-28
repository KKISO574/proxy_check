from __future__ import annotations

from collections.abc import Iterable

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
