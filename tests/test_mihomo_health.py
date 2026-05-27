from __future__ import annotations

from unittest.mock import AsyncMock, patch

import pytest

from app.core.config import Settings
from app.probes.mihomo import MihomoManager, MihomoUnavailable


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
