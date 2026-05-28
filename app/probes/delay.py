"""Mihomo-driven delay probe.

Uses the per-node delay endpoint exposed by the Mihomo external controller
(``/proxies/{name}/delay``) which itself performs a HEAD against
``probe.delay_url`` through the proxy. We treat its return value as the
canonical "delay" metric for ranking nodes.
"""

from __future__ import annotations

from app.probes.base import ProbeContext, ProbeOutcome


class DelayProber:
    metric = "delay"
    interval_seconds = 60

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        if context.mihomo_error:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=context.settings.probe.delay_url,
                    success=False,
                    error=context.mihomo_error,
                )
            ]
        if context.client is None:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=context.settings.probe.delay_url,
                    success=False,
                    error="mihomo client is not available",
                )
            ]
        try:
            delay = await context.client.delay(
                context.node.name,
                url=context.settings.probe.delay_url,
                timeout_ms=context.settings.probe.timeout_ms,
            )
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=context.settings.probe.delay_url,
                    latency_ms=delay,
                    value=delay,
                    success=True,
                )
            ]
        except Exception as exc:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=context.settings.probe.delay_url,
                    success=False,
                    error=str(exc),
                )
            ]
