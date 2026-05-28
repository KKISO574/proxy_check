"""TCP connect probe through the per-node SOCKS5 listener.

Iterates ``probe.tcp_targets`` and reports one ``ProbeOutcome`` per target.
The probe driver (``socks5_connect``) is referenced via the ``builtin``
module so tests can monkeypatch ``app.probes.builtin.socks5_connect`` to
inject deterministic latencies / failures.
"""

from __future__ import annotations

from app.probes import builtin
from app.probes.base import ProbeContext, ProbeOutcome


class TcpingProber:
    metric = "tcping"
    interval_seconds = 60

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        results: list[ProbeOutcome] = []
        for target in context.settings.probe.tcp_targets:
            if context.mihomo_error or context.node.listener_port is None:
                results.append(
                    ProbeOutcome(
                        metric=self.metric,
                        target=target.label,
                        success=False,
                        error=context.mihomo_error or "listener port is not assigned",
                    )
                )
                continue
            try:
                latency = await builtin.socks5_connect(
                    context.settings.mihomo.listener_host,
                    context.node.listener_port,
                    target.host,
                    target.port,
                    timeout_ms=context.settings.probe.timeout_ms,
                )
                results.append(
                    ProbeOutcome(
                        metric=self.metric,
                        target=target.label,
                        latency_ms=latency,
                        value=latency,
                        success=True,
                    )
                )
            except Exception as exc:
                results.append(
                    ProbeOutcome(
                        metric=self.metric,
                        target=target.label,
                        success=False,
                        error=str(exc),
                    )
                )
        return results
