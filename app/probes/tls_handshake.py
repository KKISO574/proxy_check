"""TLS handshake latency probe (cp.cloudflare.com:443) over SOCKS5.

Measures the wall-clock time spent inside ``StreamWriter.start_tls`` after
the SOCKS5 tunnel is established. The handshake helper is referenced via
the ``builtin`` module so tests can monkeypatch
``app.probes.builtin.socks5_tls_handshake``.
"""

from __future__ import annotations

from app.probes import builtin
from app.probes.base import ProbeContext, ProbeOutcome


class TlsHandshakeProber:
    metric = "tls_handshake"
    interval_seconds = 60
    target_host = "cp.cloudflare.com"
    target_port = 443

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        if context.mihomo_error or context.node.listener_port is None:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"{self.target_host}:{self.target_port}",
                    success=False,
                    error=context.mihomo_error or "listener port is not assigned",
                )
            ]
        try:
            latency = await builtin.socks5_tls_handshake(
                context.settings.mihomo.listener_host,
                context.node.listener_port,
                self.target_host,
                self.target_port,
                timeout_ms=context.settings.probe.timeout_ms,
            )
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"{self.target_host}:{self.target_port}",
                    latency_ms=latency,
                    value=latency,
                    success=True,
                )
            ]
        except Exception as exc:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"{self.target_host}:{self.target_port}",
                    success=False,
                    error=str(exc),
                )
            ]
