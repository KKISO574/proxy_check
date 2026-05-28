"""HTTPS round-trip probe (gstatic.com/generate_204) over SOCKS5.

Captures the time from "request bytes written" to "first response head
parsed", which approximates server RTT on top of the SOCKS5 + TLS path.
Resolved via the ``builtin`` module so monkeypatches of
``app.probes.builtin.socks5_http_get`` propagate to the prober at runtime.
"""

from __future__ import annotations

from app.probes import builtin
from app.probes.base import ProbeContext, ProbeOutcome


class HttpRttProber:
    metric = "http_rtt"
    interval_seconds = 60
    target_host = "www.gstatic.com"
    target_port = 443
    path = "/generate_204"

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        if context.mihomo_error or context.node.listener_port is None:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"https://{self.target_host}{self.path}",
                    success=False,
                    error=context.mihomo_error or "listener port is not assigned",
                )
            ]
        try:
            latency = await builtin.socks5_http_get(
                context.settings.mihomo.listener_host,
                context.node.listener_port,
                self.target_host,
                self.target_port,
                self.path,
                use_tls=True,
                timeout_ms=context.settings.probe.timeout_ms,
            )
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"https://{self.target_host}{self.path}",
                    latency_ms=latency,
                    value=latency,
                    success=True,
                )
            ]
        except Exception as exc:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"https://{self.target_host}{self.path}",
                    success=False,
                    error=str(exc),
                )
            ]
