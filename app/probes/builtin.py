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


# Defensive caps for SOCKS5-tunnelled HTTP responses. The response body for
# probes is intentionally tiny (generate_204 + small JSON), so any payload
# larger than 64 KiB is treated as a failure rather than buffered.
_MAX_HTTP_HEAD_BYTES = 16 * 1024
_MAX_HTTP_BODY_BYTES = 64 * 1024


def _reject_crlf(value: str, *, label: str) -> None:
    if "\r" in value or "\n" in value:
        raise ValueError(f"invalid {label}: CRLF not allowed")


async def _start_tls(
    writer: asyncio.StreamWriter,
    *,
    server_hostname: str,
    timeout: float,
) -> None:
    """Upgrade ``writer`` to TLS via the public StreamWriter.start_tls API.

    Replaces the historical hack that assigned ``writer._transport`` after
    calling ``loop.start_tls``; the public API has been available since
    Python 3.11 and handles the protocol/transport swap internally.
    """
    context = ssl.create_default_context()
    await asyncio.wait_for(
        writer.start_tls(context, server_hostname=server_hostname),
        timeout=timeout,
    )


async def _read_http_head(
    reader: asyncio.StreamReader,
    *,
    timeout: float,
) -> tuple[int, dict[str, str], bytes]:
    """Read the status line + headers; return (code, headers, leftover_body).

    The reader keeps consuming small chunks until ``\\r\\n\\r\\n`` is found
    or the head exceeds ``_MAX_HTTP_HEAD_BYTES``. Header names are
    case-folded for caller convenience.
    """
    deadline_buf = bytearray()
    while b"\r\n\r\n" not in deadline_buf:
        chunk = await asyncio.wait_for(reader.read(4096), timeout=timeout)
        if not chunk:
            break
        deadline_buf.extend(chunk)
        if len(deadline_buf) > _MAX_HTTP_HEAD_BYTES:
            raise ValueError("response head too large")
    sep = bytes(deadline_buf).find(b"\r\n\r\n")
    if sep < 0:
        raise ValueError("incomplete response head")
    raw = bytes(deadline_buf[:sep])
    body_leftover = bytes(deadline_buf[sep + 4 :])
    lines = raw.split(b"\r\n")
    status_line = lines[0].decode("iso-8859-1", errors="replace")
    parts = status_line.split(" ", 2)
    if len(parts) < 2 or not parts[0].startswith("HTTP/"):
        raise ValueError(f"invalid status line: {status_line!r}")
    try:
        status_code = int(parts[1])
    except ValueError as exc:
        raise ValueError(f"invalid status code: {parts[1]!r}") from exc
    headers: dict[str, str] = {}
    for line in lines[1:]:
        decoded = line.decode("iso-8859-1", errors="replace")
        name, sep_char, value = decoded.partition(":")
        if not sep_char:
            continue
        headers[name.strip().lower()] = value.strip()
    if headers.get("transfer-encoding", "").lower() == "chunked":
        raise ValueError("chunked transfer-encoding not supported")
    return status_code, headers, body_leftover


async def socks5_tls_handshake(
    proxy_host: str,
    proxy_port: int,
    target_host: str,
    target_port: int,
    *,
    timeout_ms: int,
) -> float:
    _reject_crlf(target_host, label="target_host")
    timeout = timeout_ms / 1000
    reader, writer, _ = await open_socks5_stream(
        proxy_host,
        proxy_port,
        target_host,
        target_port,
        timeout_ms=timeout_ms,
    )
    try:
        start = time.perf_counter()
        await _start_tls(writer, server_hostname=target_host, timeout=timeout)
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
    _reject_crlf(path, label="path")
    _reject_crlf(target_host, label="target_host")
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
            await _start_tls(writer, server_hostname=target_host, timeout=timeout)
        request = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {target_host}\r\n"
            "User-Agent: proxy-check/0.1\r\n"
            "Accept-Encoding: identity\r\n"
            "Connection: close\r\n\r\n"
        ).encode("ascii")
        start = time.perf_counter()
        writer.write(request)
        await writer.drain()
        status_code, _headers, _body = await _read_http_head(reader, timeout=timeout)
        if status_code >= 400:
            raise ValueError(f"http {status_code}")
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
    _reject_crlf(path, label="path")
    _reject_crlf(target_host, label="target_host")
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
            await _start_tls(writer, server_hostname=target_host, timeout=timeout)
        request = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {target_host}\r\n"
            "User-Agent: proxy-check/0.1\r\n"
            "Accept: application/json\r\n"
            "Accept-Encoding: identity\r\n"
            "Connection: close\r\n\r\n"
        ).encode("ascii")
        writer.write(request)
        await writer.drain()
        status_code, _headers, leftover = await _read_http_head(reader, timeout=timeout)
        if status_code != 200:
            raise ValueError(f"http {status_code}")
        body = bytearray(leftover)
        while len(body) <= _MAX_HTTP_BODY_BYTES:
            chunk = await asyncio.wait_for(
                reader.read(min(4096, _MAX_HTTP_BODY_BYTES - len(body) + 1)),
                timeout=timeout,
            )
            if not chunk:
                break
            body.extend(chunk)
        if len(body) > _MAX_HTTP_BODY_BYTES:
            raise ValueError("response body too large")
        try:
            decoded = json.loads(bytes(body).decode("utf-8"))
        except (json.JSONDecodeError, UnicodeDecodeError) as exc:
            raise ValueError(f"invalid JSON: {exc}") from None
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
            socks5_connect(
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


class ExitIpGeoProber:
    metric = "exit_geo"
    interval_seconds = 1800
    # Primary endpoint, with api.ip.sb as a fallback when the primary errors
    # (commonly ipapi.co rate limiting). Both endpoints expose subsets of the
    # same conceptual fields; ``_normalize_geo`` collapses them into our
    # canonical (exit_ip, asn, country, region, isp) tuple.
    primary_host = "ipapi.co"
    primary_path = "/json"
    fallback_host = "api.ip.sb"
    fallback_path = "/geoip"
    target_port = 443

    def __init__(self, session_factory: async_sessionmaker[AsyncSession]) -> None:
        self.session_factory = session_factory

    async def _fetch_geo(
        self,
        context: ProbeContext,
        *,
        host: str,
        path: str,
    ) -> dict[str, object]:
        return await socks5_http_get_json(
            context.settings.mihomo.listener_host,
            context.node.listener_port,
            host,
            self.target_port,
            path,
            use_tls=True,
            timeout_ms=context.settings.probe.timeout_ms,
        )

    @staticmethod
    def _normalize_geo(payload: dict[str, object]) -> dict[str, str | None]:
        # Field aliases observed across the two providers:
        #   ipapi.co  -> ip, asn, country_code, country, region, region_code, org, isp
        #   api.ip.sb -> ip, asn, country_code, country, region, isp, organization
        def first_str(*keys: str) -> str | None:
            for key in keys:
                value = payload.get(key)
                if value:
                    return str(value)
            return None

        return {
            "exit_ip": first_str("ip"),
            "asn": first_str("asn"),
            "country": first_str("country_code", "country"),
            "region": first_str("region", "region_code"),
            "isp": first_str("org", "organization", "isp"),
        }

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        primary_target = f"https://{self.primary_host}{self.primary_path}"
        if context.mihomo_error or context.node.listener_port is None:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=primary_target,
                    success=False,
                    error=context.mihomo_error or "listener port is not assigned",
                )
            ]

        endpoints = (
            (self.primary_host, self.primary_path),
            (self.fallback_host, self.fallback_path),
        )
        attempt_errors: list[str] = []
        payload: dict[str, object] | None = None
        used_target = primary_target
        for host, path in endpoints:
            try:
                payload = await self._fetch_geo(context, host=host, path=path)
                used_target = f"https://{host}{path}"
                break
            except Exception as exc:
                attempt_errors.append(f"{host}: {exc}")

        if payload is None:
            return [
                ProbeOutcome(
                    metric=self.metric,
                    target=primary_target,
                    success=False,
                    error="; ".join(attempt_errors) or "no geo endpoint succeeded",
                )
            ]

        fields = self._normalize_geo(payload)
        async with self.session_factory() as session:
            node = await session.get(type(context.node), context.node.id)
            if node is not None:
                from app.storage import repository

                await repository.upsert_node_meta(session, node, **fields)
                await session.commit()
        return [
            ProbeOutcome(
                metric=self.metric,
                target=used_target,
                success=True,
                data=json.dumps(fields),
            )
        ]
