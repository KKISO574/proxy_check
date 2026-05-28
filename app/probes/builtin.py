from __future__ import annotations

import asyncio
import json
import math
import ssl
import time

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.probes.base import ProbeContext, ProbeOutcome
from app.probes.tcping import open_socks5_stream, socks5_connect
from app.storage.models import ProbeResult


async def socks5_tls_handshake(
    proxy_host: str,
    proxy_port: int,
    target_host: str,
    target_port: int,
    *,
    timeout_ms: int,
) -> float:
    timeout = timeout_ms / 1000
    reader, writer, _ = await open_socks5_stream(
        proxy_host,
        proxy_port,
        target_host,
        target_port,
        timeout_ms=timeout_ms,
    )
    try:
        context = ssl.create_default_context()
        transport = writer.transport
        protocol = asyncio.StreamReaderProtocol(reader)
        loop = asyncio.get_running_loop()
        start = time.perf_counter()
        tls_transport = await asyncio.wait_for(
            loop.start_tls(
                transport,
                protocol,
                context,
                server_hostname=target_host,
            ),
            timeout=timeout,
        )
        writer._transport = tls_transport  # type: ignore[attr-defined]
        return (time.perf_counter() - start) * 1000
    finally:
        writer.close()
        await writer.wait_closed()


async def socks5_http_get(
    proxy_host: str,
    proxy_port: int,
    target_host: str,
    target_port: int,
    path: str,
    *,
    use_tls: bool,
    timeout_ms: int,
) -> float:
    timeout = timeout_ms / 1000
    reader, writer, _ = await open_socks5_stream(
        proxy_host,
        proxy_port,
        target_host,
        target_port,
        timeout_ms=timeout_ms,
    )
    try:
        if use_tls:
            context = ssl.create_default_context()
            transport = writer.transport
            protocol = asyncio.StreamReaderProtocol(reader)
            loop = asyncio.get_running_loop()
            tls_transport = await asyncio.wait_for(
                loop.start_tls(
                    transport,
                    protocol,
                    context,
                    server_hostname=target_host,
                ),
                timeout=timeout,
            )
            writer._transport = tls_transport  # type: ignore[attr-defined]
        request = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {target_host}\r\n"
            "User-Agent: proxy-check/0.1\r\n"
            "Connection: close\r\n\r\n"
        ).encode("ascii")
        start = time.perf_counter()
        writer.write(request)
        await writer.drain()
        await asyncio.wait_for(reader.read(1), timeout=timeout)
        return (time.perf_counter() - start) * 1000
    finally:
        writer.close()
        await writer.wait_closed()


async def socks5_http_get_json(
    proxy_host: str,
    proxy_port: int,
    target_host: str,
    target_port: int,
    path: str,
    *,
    use_tls: bool,
    timeout_ms: int,
) -> dict[str, object]:
    timeout = timeout_ms / 1000
    reader, writer, _ = await open_socks5_stream(
        proxy_host,
        proxy_port,
        target_host,
        target_port,
        timeout_ms=timeout_ms,
    )
    try:
        if use_tls:
            context = ssl.create_default_context()
            transport = writer.transport
            protocol = asyncio.StreamReaderProtocol(reader)
            loop = asyncio.get_running_loop()
            tls_transport = await asyncio.wait_for(
                loop.start_tls(
                    transport,
                    protocol,
                    context,
                    server_hostname=target_host,
                ),
                timeout=timeout,
            )
            writer._transport = tls_transport  # type: ignore[attr-defined]
        request = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {target_host}\r\n"
            "User-Agent: proxy-check/0.1\r\n"
            "Accept: application/json\r\n"
            "Connection: close\r\n\r\n"
        ).encode("ascii")
        writer.write(request)
        await writer.drain()
        payload = await asyncio.wait_for(reader.read(-1), timeout=timeout)
        _, _, body = payload.partition(b"\r\n\r\n")
        decoded = json.loads(body.decode("utf-8"))
        if not isinstance(decoded, dict):
            raise ValueError("JSON response is not an object")
        return decoded
    finally:
        writer.close()
        await writer.wait_closed()


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
                latency = await socks5_connect(
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
            latency = await socks5_tls_handshake(
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
            latency = await socks5_http_get(
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
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target="delay:last_samples",
                    success=False,
                    error="not enough delay samples",
                    data=json.dumps({"samples": len(samples)}),
                )
            ]
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
        target = context.settings.probe.tcp_targets[0]
        failed = 0
        latencies: list[float] = []
        for _ in range(self.samples):
            try:
                latency = await socks5_connect(
                    context.settings.mihomo.listener_host,
                    context.node.listener_port,
                    target.host,
                    target.port,
                    timeout_ms=context.settings.probe.timeout_ms,
                )
                latencies.append(latency)
            except Exception:
                failed += 1
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


class ExitIpGeoProber:
    metric = "exit_geo"
    interval_seconds = 1800
    target_host = "ipapi.co"
    target_port = 443
    path = "/json"

    def __init__(self, session_factory: async_sessionmaker[AsyncSession]) -> None:
        self.session_factory = session_factory

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
            payload = await socks5_http_get_json(
                context.settings.mihomo.listener_host,
                context.node.listener_port,
                self.target_host,
                self.target_port,
                self.path,
                use_tls=True,
                timeout_ms=context.settings.probe.timeout_ms,
            )
            exit_ip = str(payload.get("ip") or "") or None
            asn = str(payload.get("asn") or "") or None
            country = str(payload.get("country_code") or payload.get("country") or "") or None
            region = str(payload.get("region") or payload.get("region_code") or "") or None
            isp = str(payload.get("org") or payload.get("isp") or "") or None
            async with self.session_factory() as session:
                node = await session.get(type(context.node), context.node.id)
                if node is not None:
                    from app.storage import repository

                    await repository.upsert_node_meta(
                        session,
                        node,
                        exit_ip=exit_ip,
                        asn=asn,
                        country=country,
                        region=region,
                        isp=isp,
                    )
                    await session.commit()
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=f"https://{self.target_host}{self.path}",
                    success=True,
                    data=json.dumps(
                        {
                            "exit_ip": exit_ip,
                            "asn": asn,
                            "country": country,
                            "region": region,
                            "isp": isp,
                        }
                    ),
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
