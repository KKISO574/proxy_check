"""Unit tests for the SOCKS5-tunnelled HTTP helpers in ``app/probes/builtin``.

These tests stub ``open_socks5_stream`` and feed canned bytes through an
``asyncio.StreamReader`` so the hardening defenses (size cap, status code
validation, ``Accept-Encoding: identity``, chunked rejection, CRLF
guards, JSON error sanitisation) can be exercised without binding real
sockets — useful since the test sandbox forbids ``socket.bind``.
"""

from __future__ import annotations

import asyncio

import pytest

from app.probes import builtin


class _FakeWriter:
    def __init__(self) -> None:
        self.written = bytearray()
        self.closed = False

    def write(self, data: bytes) -> None:
        self.written.extend(data)

    async def drain(self) -> None:
        return None

    def close(self) -> None:
        self.closed = True

    async def wait_closed(self) -> None:
        return None

    async def start_tls(self, *_args: object, **_kwargs: object) -> None:
        return None


def _patched_open_stream(response: bytes):
    async def _fake(*_args: object, **_kwargs: object):
        reader = asyncio.StreamReader()
        reader.feed_data(response)
        reader.feed_eof()
        return reader, _FakeWriter(), 0.0

    return _fake


@pytest.mark.asyncio
async def test_socks5_http_get_json_returns_parsed_object(monkeypatch):
    response = (
        b"HTTP/1.1 200 OK\r\n"
        b"Content-Type: application/json\r\n\r\n"
        b'{"ip":"203.0.113.10","org":"Example"}'
    )
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    result = await builtin.socks5_http_get_json(
        "127.0.0.1", 1080, "example.com", 80, "/json",
        use_tls=False, timeout_ms=1000,
    )

    assert result == {"ip": "203.0.113.10", "org": "Example"}


@pytest.mark.asyncio
async def test_socks5_http_get_json_rejects_non_200(monkeypatch):
    response = (
        b"HTTP/1.1 500 Server Error\r\n"
        b"Content-Length: 4\r\n\r\n"
        b"oops"
    )
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    with pytest.raises(ValueError, match="http 500"):
        await builtin.socks5_http_get_json(
            "127.0.0.1", 1080, "example.com", 80, "/json",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_json_rejects_chunked(monkeypatch):
    response = (
        b"HTTP/1.1 200 OK\r\n"
        b"Transfer-Encoding: chunked\r\n\r\n"
        b"4\r\nabcd\r\n0\r\n\r\n"
    )
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    with pytest.raises(ValueError, match="chunked"):
        await builtin.socks5_http_get_json(
            "127.0.0.1", 1080, "example.com", 80, "/json",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_json_rejects_oversized_body(monkeypatch):
    big_body = b"a" * (64 * 1024 + 4096)
    response = (
        b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" + big_body
    )
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    with pytest.raises(ValueError, match="too large"):
        await builtin.socks5_http_get_json(
            "127.0.0.1", 1080, "example.com", 80, "/json",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_json_rejects_truncated_head(monkeypatch):
    # Missing the trailing CRLF/CRLF — head will never terminate.
    response = b"HTTP/1.1 200 OK\r\nContent-Type: app"
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    with pytest.raises(ValueError, match="incomplete response head"):
        await builtin.socks5_http_get_json(
            "127.0.0.1", 1080, "example.com", 80, "/json",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_json_invalid_json_message(monkeypatch):
    response = b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\nnot-json"
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    with pytest.raises(ValueError, match="invalid JSON"):
        await builtin.socks5_http_get_json(
            "127.0.0.1", 1080, "example.com", 80, "/json",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_json_sends_identity_encoding(monkeypatch):
    captured: dict[str, _FakeWriter] = {}

    async def fake_open(*_args: object, **_kwargs: object):
        reader = asyncio.StreamReader()
        reader.feed_data(b"HTTP/1.1 200 OK\r\n\r\n{}")
        reader.feed_eof()
        writer = _FakeWriter()
        captured["writer"] = writer
        return reader, writer, 0.0

    monkeypatch.setattr(builtin, "open_socks5_stream", fake_open)

    await builtin.socks5_http_get_json(
        "127.0.0.1", 1080, "example.com", 80, "/json",
        use_tls=False, timeout_ms=1000,
    )

    request = bytes(captured["writer"].written)
    assert b"Accept-Encoding: identity\r\n" in request


@pytest.mark.asyncio
async def test_socks5_http_get_accepts_204(monkeypatch):
    response = b"HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n"
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    latency = await builtin.socks5_http_get(
        "127.0.0.1", 1080, "example.com", 80, "/204",
        use_tls=False, timeout_ms=1000,
    )

    assert isinstance(latency, float)
    assert latency >= 0


@pytest.mark.asyncio
async def test_socks5_http_get_rejects_4xx(monkeypatch):
    response = b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"
    monkeypatch.setattr(builtin, "open_socks5_stream", _patched_open_stream(response))

    with pytest.raises(ValueError, match="http 404"):
        await builtin.socks5_http_get(
            "127.0.0.1", 1080, "example.com", 80, "/204",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_sends_identity_encoding(monkeypatch):
    captured: dict[str, _FakeWriter] = {}

    async def fake_open(*_args: object, **_kwargs: object):
        reader = asyncio.StreamReader()
        reader.feed_data(b"HTTP/1.1 204 No Content\r\n\r\n")
        reader.feed_eof()
        writer = _FakeWriter()
        captured["writer"] = writer
        return reader, writer, 0.0

    monkeypatch.setattr(builtin, "open_socks5_stream", fake_open)

    await builtin.socks5_http_get(
        "127.0.0.1", 1080, "example.com", 80, "/204",
        use_tls=False, timeout_ms=1000,
    )

    request = bytes(captured["writer"].written)
    assert b"Accept-Encoding: identity\r\n" in request


@pytest.mark.asyncio
async def test_socks5_http_get_rejects_crlf_in_path():
    with pytest.raises(ValueError, match="CRLF"):
        await builtin.socks5_http_get(
            "127.0.0.1", 1080, "example.com", 80,
            "/inj\r\nHost: evil",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_json_rejects_crlf_in_path():
    with pytest.raises(ValueError, match="CRLF"):
        await builtin.socks5_http_get_json(
            "127.0.0.1", 1080, "example.com", 80,
            "/inj\r\nHost: evil",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_http_get_rejects_crlf_in_host():
    with pytest.raises(ValueError, match="CRLF"):
        await builtin.socks5_http_get(
            "127.0.0.1", 1080, "evil\r\nFoo: bar", 80, "/json",
            use_tls=False, timeout_ms=1000,
        )


@pytest.mark.asyncio
async def test_socks5_tls_handshake_rejects_crlf_in_host():
    with pytest.raises(ValueError, match="CRLF"):
        await builtin.socks5_tls_handshake(
            "127.0.0.1", 1080, "evil\r\nFoo: bar", 443,
            timeout_ms=1000,
        )
