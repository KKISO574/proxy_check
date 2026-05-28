"""SOCKS5-tunnelled HTTP/TLS helpers used by probers.

These helpers piggyback on a Mihomo per-node SOCKS5 listener: a single
TCP connection performs the SOCKS5 handshake, optionally upgrades to
TLS, and either measures the time to first byte (``socks5_http_get``)
or parses a bounded JSON response (``socks5_http_get_json``).

All helpers enforce the same hardening rules:

- ``Accept-Encoding: identity`` to prevent compressed responses
- Reject ``Transfer-Encoding: chunked`` (we don't decode it)
- Hard caps on response head (16 KiB) and body (64 KiB)
- HTTP status code validation (non-200 → ``ValueError``)
- CRLF guard on caller-supplied ``path`` and ``target_host``
- ``StreamWriter.start_tls`` (Python 3.11+ public API), no private
  ``_transport`` mutation

These helpers are also re-exported via ``app.probes.builtin`` so existing
tests that monkeypatch ``app.probes.builtin.socks5_*`` continue to work.
"""

from __future__ import annotations

import asyncio
import json
import ssl
import time

# Defensive caps for SOCKS5-tunnelled HTTP responses. The response body for
# probes is intentionally tiny (generate_204 + small JSON), so any payload
# larger than 64 KiB is treated as a failure rather than buffered.
_MAX_HTTP_HEAD_BYTES = 16 * 1024
_MAX_HTTP_BODY_BYTES = 64 * 1024


async def _open_socks5_stream(
    proxy_host: str,
    proxy_port: int,
    target_host: str,
    target_port: int,
    *,
    timeout_ms: int,
):
    # Resolve through the compatibility shim at call time so existing tests
    # and integrations that monkeypatch app.probes.builtin.open_socks5_stream
    # continue to affect these helpers after the module split.
    from app.probes import builtin

    return await builtin.open_socks5_stream(
        proxy_host,
        proxy_port,
        target_host,
        target_port,
        timeout_ms=timeout_ms,
    )


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
    reader, writer, _ = await _open_socks5_stream(
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
    reader, writer, _ = await _open_socks5_stream(
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
    reader, writer, _ = await _open_socks5_stream(
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
