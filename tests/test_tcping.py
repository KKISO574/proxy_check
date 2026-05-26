from __future__ import annotations

import asyncio
import socket
import struct

import pytest

from app.probes.tcping import TcpingError, socks5_connect


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


async def socks_server(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
    try:
        await reader.readexactly(3)
        writer.write(b"\x05\x00")
        await writer.drain()

        header = await reader.readexactly(4)
        atyp = header[3]
        if atyp == 3:
            length = (await reader.readexactly(1))[0]
            await reader.readexactly(length)
        elif atyp == 1:
            await reader.readexactly(4)
        await reader.readexactly(2)

        writer.write(b"\x05\x00\x00\x01" + b"\x00\x00\x00\x00" + struct.pack("!H", 0))
        await writer.drain()
    finally:
        writer.close()
        await writer.wait_closed()


@pytest.mark.asyncio
async def test_socks5_connect_success():
    port = free_port()
    server = await asyncio.start_server(socks_server, "127.0.0.1", port)
    try:
        latency = await socks5_connect("127.0.0.1", port, "1.1.1.1", 443, timeout_ms=1000)
    finally:
        server.close()
        await server.wait_closed()

    assert latency >= 0


@pytest.mark.asyncio
async def test_socks5_connect_failure():
    port = free_port()
    with pytest.raises(TcpingError):
        await socks5_connect("127.0.0.1", port, "1.1.1.1", 443, timeout_ms=100)

