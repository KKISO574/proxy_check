"""Backwards-compatible re-export shim for the v2 prober suite.

Per-prober logic now lives in dedicated modules under ``app.probes`` (see
``delay``, ``tcping_prober``, ``tls_handshake``, ``http_rtt``, ``jitter``,
``packet_loss``, ``exit_geo``); SOCKS5 HTTP/TLS helpers live in
``app.probes.socks5_http`` and the raw connect helper stays in
``app.probes.tcping``.

This shim keeps every prior public symbol resolvable as a *module attribute*
on ``app.probes.builtin`` so existing imports keep working and, more
importantly, so test monkeypatches like
``monkeypatch.setattr("app.probes.builtin.socks5_http_get", fake)`` continue
to take effect — per-prober modules dereference the helpers via
``from app.probes import builtin`` and call ``builtin.socks5_*`` at runtime
specifically to honour these patches.
"""

from __future__ import annotations

from app.probes.delay import DelayProber
from app.probes.exit_geo import ExitIpGeoProber
from app.probes.http_rtt import HttpRttProber
from app.probes.jitter import JitterProber
from app.probes.packet_loss import PacketLossProber
from app.probes.socks5_http import (
    socks5_http_get,
    socks5_http_get_json,
    socks5_tls_handshake,
)
from app.probes.tcping import open_socks5_stream, socks5_connect
from app.probes.tcping_prober import TcpingProber
from app.probes.tls_handshake import TlsHandshakeProber

__all__ = [
    "DelayProber",
    "ExitIpGeoProber",
    "HttpRttProber",
    "JitterProber",
    "PacketLossProber",
    "TcpingProber",
    "TlsHandshakeProber",
    "socks5_connect",
    "socks5_http_get",
    "socks5_http_get_json",
    "socks5_tls_handshake",
    "open_socks5_stream",
]
