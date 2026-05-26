from __future__ import annotations

import asyncio
import struct
import time


class TcpingError(RuntimeError):
    pass


async def _read_exact(reader: asyncio.StreamReader, size: int) -> bytes:
    data = await reader.readexactly(size)
    return data


async def socks5_connect(
    proxy_host: str,
    proxy_port: int,
    target_host: str,
    target_port: int,
    *,
    timeout_ms: int,
) -> float:
    timeout = timeout_ms / 1000
    start = time.perf_counter()
    writer: asyncio.StreamWriter | None = None
    try:
        reader, writer = await asyncio.wait_for(
            asyncio.open_connection(proxy_host, proxy_port),
            timeout=timeout,
        )
        writer.write(b"\x05\x01\x00")
        await writer.drain()
        handshake = await asyncio.wait_for(_read_exact(reader, 2), timeout=timeout)
        if handshake != b"\x05\x00":
            raise TcpingError(f"SOCKS5 handshake rejected: {handshake!r}")

        host_bytes = target_host.encode("idna")
        if len(host_bytes) > 255:
            raise TcpingError("target host is too long")
        request = (
            b"\x05\x01\x00\x03"
            + bytes([len(host_bytes)])
            + host_bytes
            + struct.pack("!H", target_port)
        )
        writer.write(request)
        await writer.drain()

        header = await asyncio.wait_for(_read_exact(reader, 4), timeout=timeout)
        if header[1] != 0:
            raise TcpingError(f"SOCKS5 connect failed with code {header[1]}")
        atyp = header[3]
        if atyp == 1:
            await asyncio.wait_for(_read_exact(reader, 4), timeout=timeout)
        elif atyp == 3:
            length = (await asyncio.wait_for(_read_exact(reader, 1), timeout=timeout))[0]
            await asyncio.wait_for(_read_exact(reader, length), timeout=timeout)
        elif atyp == 4:
            await asyncio.wait_for(_read_exact(reader, 16), timeout=timeout)
        else:
            raise TcpingError(f"unknown SOCKS5 address type {atyp}")
        await asyncio.wait_for(_read_exact(reader, 2), timeout=timeout)
        return (time.perf_counter() - start) * 1000
    except TimeoutError as exc:
        raise TcpingError("tcping timeout") from exc
    except OSError as exc:
        raise TcpingError(str(exc)) from exc
    finally:
        if writer is not None:
            writer.close()
            await writer.wait_closed()

