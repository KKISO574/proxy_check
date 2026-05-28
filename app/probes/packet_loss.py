"""Packet-loss probe via repeated SOCKS5 TCP connects.

Issues ``samples`` parallel SOCKS5 connect attempts against the first
configured ``probe.tcp_targets`` entry and reports the failure ratio as a
percentage. The connect helper is referenced via ``app.probes.builtin``
so tests can monkeypatch ``app.probes.builtin.socks5_connect`` to inject
deterministic failure patterns and measure concurrent dispatch.
"""

from __future__ import annotations

import asyncio
import json

from app.probes import builtin
from app.probes.base import ProbeContext, ProbeOutcome


class PacketLossProber:
    metric = "packet_loss"
    interval_seconds = 300

    def __init__(self, *, samples: int = 20) -> None:
        self.samples = samples

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        if context.mihomo_error or context.node.listener_port is None:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target="tcping:default",
                    success=False,
                    error=context.mihomo_error or "listener port is not assigned",
                    data=json.dumps({"sent": 0, "failed": 0}),
                )
            ]
        if not context.settings.probe.tcp_targets:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target="tcping:default",
                    success=False,
                    error="no tcp_targets configured",
                    data=json.dumps({"sent": 0, "failed": 0}),
                )
            ]
        target = context.settings.probe.tcp_targets[0]
        timeout_ms = context.settings.probe.timeout_ms
        # Run all samples concurrently; deadline is roughly a quarter of
        # the serial worst case (samples overlap heavily), but never less
        # than a single sample's own timeout.
        deadline = max(self.samples * timeout_ms / 1000 / 4, timeout_ms / 1000)
        coros = [
            builtin.socks5_connect(
                context.settings.mihomo.listener_host,
                context.node.listener_port,
                target.host,
                target.port,
                timeout_ms=timeout_ms,
            )
            for _ in range(self.samples)
        ]
        try:
            outcomes: list[float | BaseException] = await asyncio.wait_for(
                asyncio.gather(*coros, return_exceptions=True),
                timeout=deadline,
            )
        except asyncio.TimeoutError:
            # Overall deadline blew up — treat every probe as failed.
            failed = self.samples
            latencies: list[float] = []
        else:
            failed = sum(1 for item in outcomes if isinstance(item, BaseException))
            latencies = [
                float(item) for item in outcomes if isinstance(item, (int, float))
            ]
        loss = (failed / self.samples) * 100 if self.samples else 0.0
        return [
            ProbeOutcome(
                metric=self.metric,
                target=target.label,
                value=loss,
                success=failed < self.samples,
                data=json.dumps(
                    {
                        "sent": self.samples,
                        "failed": failed,
                        "avg_latency_ms": sum(latencies) / len(latencies) if latencies else None,
                    }
                ),
            )
        ]
