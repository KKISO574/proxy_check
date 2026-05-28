from __future__ import annotations

import asyncio
import logging
from unittest.mock import AsyncMock, patch

import pytest

from app.core.config import Settings
from app.probes.mihomo import MihomoClient, MihomoManager, MihomoUnavailable


class FakeResponse:
    def __init__(self, status: int) -> None:
        self.status = status

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, tb):
        return None


class FakeSession:
    """Simulates aiohttp.ClientSession returning the next configured response per call."""

    def __init__(self, statuses: list[int | type[Exception]]) -> None:
        self.statuses = list(statuses)
        self.calls = 0

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, tb):
        return None

    def get(self, url: str):  # noqa: ARG002
        self.calls += 1
        if not self.statuses:
            return FakeResponse(200)
        item = self.statuses.pop(0)
        if isinstance(item, type) and issubclass(item, Exception):
            raise item("connection refused")
        return FakeResponse(item)


@pytest.mark.asyncio
async def test_wait_ready_returns_after_controller_responds():
    manager = MihomoManager(Settings())
    # First two calls fail with connection error, third returns 200.
    fake = FakeSession([ConnectionError, ConnectionError, 200])
    with patch("app.probes.mihomo.aiohttp.ClientSession", return_value=fake):
        await manager._wait_ready(attempts=10, interval_seconds=0.001)
    assert fake.calls == 3


@pytest.mark.asyncio
async def test_wait_ready_raises_when_controller_never_ready():
    manager = MihomoManager(Settings())
    fake = FakeSession([ConnectionError] * 5)
    with patch("app.probes.mihomo.aiohttp.ClientSession", return_value=fake):
        with pytest.raises(MihomoUnavailable, match="controller not ready"):
            await manager._wait_ready(attempts=5, interval_seconds=0.001)
    assert fake.calls == 5


@pytest.mark.asyncio
async def test_wait_ready_raises_when_process_exits_early():
    manager = MihomoManager(Settings())
    # Simulate a process whose returncode is already set (i.e. it has exited).
    manager.process = AsyncMock()
    manager.process.returncode = 1
    fake = FakeSession([ConnectionError])
    with patch("app.probes.mihomo.aiohttp.ClientSession", return_value=fake):
        with pytest.raises(MihomoUnavailable, match="mihomo exited"):
            await manager._wait_ready(attempts=5, interval_seconds=0.001)


class FakeDelayResponse:
    status = 200

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, tb):
        return None

    def raise_for_status(self) -> None:
        return None

    async def json(self) -> dict[str, int]:
        return {"delay": 42}


class FakeReusableClientSession:
    instances: list["FakeReusableClientSession"] = []

    def __init__(self, *args, **kwargs) -> None:  # noqa: ANN002, ANN003
        self.closed = False
        self.get_calls = 0
        self.close_calls = 0
        FakeReusableClientSession.instances.append(self)

    def get(self, *args, **kwargs):  # noqa: ANN002, ANN003
        self.get_calls += 1
        return FakeDelayResponse()

    async def close(self) -> None:
        self.close_calls += 1
        self.closed = True


@pytest.mark.asyncio
async def test_mihomo_client_reuses_session_and_closes_it():
    FakeReusableClientSession.instances = []
    client = MihomoClient("http://127.0.0.1:9090", "secret", timeout_ms=1000)
    with patch("app.probes.mihomo.aiohttp.ClientSession", FakeReusableClientSession):
        assert await client.delay("node-a", url="https://example.com", timeout_ms=1000) == 42
        assert await client.delay("node-a", url="https://example.com", timeout_ms=1000) == 42
        await client.aclose()

    assert len(FakeReusableClientSession.instances) == 1
    assert FakeReusableClientSession.instances[0].get_calls == 2
    assert FakeReusableClientSession.instances[0].close_calls == 1


@pytest.mark.asyncio
async def test_mihomo_manager_consumes_process_stream(caplog):
    manager = MihomoManager(Settings())
    reader = asyncio.StreamReader()
    reader.feed_data(b"ready\n")
    reader.feed_eof()

    caplog.set_level(logging.INFO, logger="app.probes.mihomo")
    await manager._consume_stream(reader, "stdout", logging.INFO)

    assert "mihomo stdout: ready" in caplog.text
