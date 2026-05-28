from __future__ import annotations

import asyncio
import json
import time
from datetime import datetime, timedelta, timezone

import pytest
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from app.core.config import Settings
from app.probes.builtin import (
    ExitIpGeoProber,
    HttpRttProber,
    JitterProber,
    PacketLossProber,
    TlsHandshakeProber,
)
from app.probes.base import ProbeContext, ProbeOutcome, Prober
from app.probes.registry import ProbeRegistry
from app.services.probe_service import ProbeService
from app.storage import repository
from app.storage.models import Base, Node, NodeMeta, ProbeResult


class StaticProber(Prober):
    metric = "custom_metric"
    interval_seconds = 60

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:
        return [
            ProbeOutcome(
                metric=self.metric,
                target=context.node.name,
                latency_ms=None,
                value=42.5,
                data='{"source":"test"}',
                success=True,
                error=None,
            )
        ]


@pytest.mark.asyncio
async def test_probe_result_supports_value_and_data_and_node_meta_upsert():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    try:
        async with session_factory() as session:
            node = Node(name="node-a", status="available")
            session.add(node)
            await session.flush()
            await repository.add_probe_result(
                session,
                node,
                metric="packet_loss",
                target="tcping:default",
                latency_ms=None,
                value=12.5,
                data='{"sent":20,"failed":3}',
                success=True,
                error=None,
            )
            await repository.upsert_node_meta(
                session,
                node,
                exit_ip="203.0.113.10",
                asn="AS64500",
                country="US",
                region="California",
                isp="Example ISP",
            )
            await session.commit()

            result = (await session.execute(ProbeResult.__table__.select())).mappings().one()
            assert result["value"] == 12.5
            assert result["data"] == '{"sent":20,"failed":3}'

            meta = await repository.get_node_meta(session, node.id)
            assert isinstance(meta, NodeMeta)
            assert meta.exit_ip == "203.0.113.10"
            assert meta.asn == "AS64500"
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_derived_jitter_uses_last_delay_samples():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    try:
        async with session_factory() as session:
            node = Node(name="node-a", status="available")
            session.add(node)
            await session.flush()
            for value in [100.0, 110.0, 90.0, 105.0, 95.0]:
                await repository.add_probe_result(
                    session,
                    node,
                    metric="delay",
                    target="https://cp.cloudflare.com/generate_204",
                    latency_ms=value,
                    value=value,
                    success=True,
                    error=None,
                )
            await session.commit()

            prober = JitterProber(session_factory, sample_size=5)
            outcome = (await prober.probe(ProbeContext(node, Settings(), None)))[0]

            assert outcome.metric == "jitter"
            assert outcome.success is True
            assert outcome.latency_ms is None
            assert outcome.value == pytest.approx(7.071, rel=0.01)
            assert json.loads(outcome.data or "{}")["samples"] == 5
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_packet_loss_runs_tcping_series_and_records_percentage(monkeypatch):
    calls: list[tuple[str, int]] = []

    async def fake_socks5_connect(
        proxy_host: str,
        proxy_port: int,
        target_host: str,
        target_port: int,
        *,
        timeout_ms: int,
    ) -> float:
        calls.append((target_host, target_port))
        if len(calls) in {2, 5}:
            raise RuntimeError("connect failed")
        return 20.0

    monkeypatch.setattr("app.probes.builtin.socks5_connect", fake_socks5_connect)
    settings = Settings()
    node = Node(name="node-a", listener_port=20000)
    prober = PacketLossProber(samples=5)

    outcome = (await prober.probe(ProbeContext(node, settings, None)))[0]

    assert outcome.metric == "packet_loss"
    assert outcome.success is True
    assert outcome.value == 40.0
    payload = json.loads(outcome.data or "{}")
    assert payload["sent"] == 5
    assert payload["failed"] == 2


@pytest.mark.asyncio
async def test_packet_loss_returns_error_when_tcp_targets_empty(monkeypatch):
    async def fake_socks5_connect(*_args: object, **_kwargs: object) -> float:
        raise AssertionError("socks5_connect must not be called when targets are empty")

    monkeypatch.setattr("app.probes.builtin.socks5_connect", fake_socks5_connect)
    settings = Settings()
    settings.probe.tcp_targets = []
    node = Node(name="node-a", listener_port=20000)

    outcome = (await PacketLossProber(samples=5).probe(ProbeContext(node, settings, None)))[0]

    assert outcome.success is False
    assert outcome.error == "no tcp_targets configured"
    payload = json.loads(outcome.data or "{}")
    assert payload["sent"] == 0
    assert payload["failed"] == 0


@pytest.mark.asyncio
async def test_packet_loss_runs_samples_concurrently(monkeypatch):
    sample_delay = 0.05
    samples = 10

    async def slow_socks5_connect(*_args: object, **_kwargs: object) -> float:
        await asyncio.sleep(sample_delay)
        return 12.5

    monkeypatch.setattr("app.probes.builtin.socks5_connect", slow_socks5_connect)
    settings = Settings()
    node = Node(name="node-a", listener_port=20000)
    prober = PacketLossProber(samples=samples)

    started = time.perf_counter()
    outcome = (await prober.probe(ProbeContext(node, settings, None)))[0]
    elapsed = time.perf_counter() - started

    # Serial would take >= samples * sample_delay (0.5s); concurrent execution
    # should finish well under half of that. Allow generous slack for CI jitter.
    assert elapsed < (samples * sample_delay) / 2, (
        f"PacketLossProber appears to be running serially (elapsed={elapsed:.3f}s)"
    )
    assert outcome.metric == "packet_loss"
    assert outcome.success is True
    assert outcome.value == 0.0
    payload = json.loads(outcome.data or "{}")
    assert payload["sent"] == samples
    assert payload["failed"] == 0


@pytest.mark.asyncio
async def test_tls_and_http_probers_record_latency(monkeypatch):
    async def fake_tls(*args, **kwargs) -> float:
        return 31.5

    async def fake_http(*args, **kwargs) -> float:
        return 55.0

    monkeypatch.setattr("app.probes.builtin.socks5_tls_handshake", fake_tls)
    monkeypatch.setattr("app.probes.builtin.socks5_http_get", fake_http)

    settings = Settings()
    node = Node(name="node-a", listener_port=20000)
    context = ProbeContext(node, settings, None)

    tls = (await TlsHandshakeProber().probe(context))[0]
    http = (await HttpRttProber().probe(context))[0]

    assert tls.metric == "tls_handshake"
    assert tls.latency_ms == 31.5
    assert tls.value == 31.5
    assert http.metric == "http_rtt"
    assert http.latency_ms == 55.0
    assert http.value == 55.0


@pytest.mark.asyncio
async def test_exit_ip_geo_prober_upserts_node_meta(monkeypatch):
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    async def fake_json(*args, **kwargs) -> dict[str, object]:
        return {
            "ip": "203.0.113.10",
            "asn": "AS64500",
            "country_code": "US",
            "region": "California",
            "org": "Example ISP",
        }

    monkeypatch.setattr("app.probes.builtin.socks5_http_get_json", fake_json)

    try:
        async with session_factory() as session:
            node = Node(name="node-a", listener_port=20000)
            session.add(node)
            await session.commit()

            prober = ExitIpGeoProber(session_factory)
            outcome = (await prober.probe(ProbeContext(node, Settings(), None)))[0]

            assert outcome.metric == "exit_geo"
            assert outcome.success is True
            meta = await repository.get_node_meta(session, node.id)
            assert meta is not None
            assert meta.exit_ip == "203.0.113.10"
            assert meta.asn == "AS64500"
            assert meta.country == "US"
            assert meta.region == "California"
            assert meta.isp == "Example ISP"
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_exit_ip_geo_prober_falls_back_when_primary_endpoint_fails(monkeypatch):
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    calls: list[str] = []

    async def fake_json(host_arg, port_arg, host, port, path, **kwargs):
        calls.append(host)
        if host == "ipapi.co":
            raise RuntimeError("rate limited")
        # api.ip.sb shape: organization instead of org, otherwise overlaps.
        return {
            "ip": "198.51.100.7",
            "asn": "AS65001",
            "country_code": "JP",
            "region": "Tokyo",
            "organization": "Backup ISP",
        }

    monkeypatch.setattr("app.probes.builtin.socks5_http_get_json", fake_json)

    try:
        async with session_factory() as session:
            node = Node(name="node-fallback", listener_port=20000)
            session.add(node)
            await session.commit()

            prober = ExitIpGeoProber(session_factory)
            outcome = (await prober.probe(ProbeContext(node, Settings(), None)))[0]

            assert calls == ["ipapi.co", "api.ip.sb"]
            assert outcome.success is True
            assert outcome.target == "https://api.ip.sb/geoip"
            meta = await repository.get_node_meta(session, node.id)
            assert meta is not None
            assert meta.exit_ip == "198.51.100.7"
            assert meta.asn == "AS65001"
            assert meta.country == "JP"
            assert meta.region == "Tokyo"
            assert meta.isp == "Backup ISP"
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_exit_ip_geo_prober_records_failure_when_both_endpoints_fail(monkeypatch):
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    async def fake_json(host_arg, port_arg, host, port, path, **kwargs):
        raise RuntimeError(f"{host} down")

    monkeypatch.setattr("app.probes.builtin.socks5_http_get_json", fake_json)

    try:
        async with session_factory() as session:
            node = Node(name="node-down", listener_port=20000)
            session.add(node)
            await session.commit()

            prober = ExitIpGeoProber(session_factory)
            outcome = (await prober.probe(ProbeContext(node, Settings(), None)))[0]

            assert outcome.success is False
            assert "ipapi.co" in (outcome.error or "")
            assert "api.ip.sb" in (outcome.error or "")
            meta = await repository.get_node_meta(session, node.id)
            assert meta is None
    finally:
        await engine.dispose()


@pytest.mark.asyncio
async def test_probe_service_runs_registered_probers_instead_of_hard_coded_metrics():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    try:
        async with session_factory() as session:
            node = Node(name="node-a", listener_port=20000, status="unknown")
            session.add(node)
            await session.commit()

        registry = ProbeRegistry([StaticProber()])
        settings = Settings()
        settings.probe.dimensions = ["custom_metric"]
        service = ProbeService(settings, session_factory, registry=registry)

        count, errors = await service._probe_node(
            1,
            client=None,
            semaphore=asyncio.Semaphore(1),
            mihomo_error=None,
        )

        assert count == 1
        assert errors == 0
        async with session_factory() as session:
            latest = await repository.nodes_with_latest_metrics(
                session,
                metrics=["custom_metric"],
            )
            metric = latest[0]["metrics"]["custom_metric"]
            assert metric.value == 42.5
            assert metric.data == '{"source":"test"}'
    finally:
        await engine.dispose()


class _GatedProber(Prober):
    """Test prober that records `interval_seconds` for the gate test."""

    def __init__(self, metric: str, interval: int) -> None:
        self.metric = metric
        self.interval_seconds = interval

    async def probe(self, context: ProbeContext) -> list[ProbeOutcome]:  # pragma: no cover
        return []


def test_registry_due_returns_probers_without_recorded_success():
    registry = ProbeRegistry(
        [
            _GatedProber("delay", 60),
            _GatedProber("exit_geo", 1800),
        ]
    )
    due = registry.due(None, last_seen={})
    assert {prober.metric for prober in due} == {"delay", "exit_geo"}


def test_registry_due_skips_probers_within_interval_window():
    now = datetime(2026, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
    registry = ProbeRegistry(
        [
            _GatedProber("delay", 60),
            _GatedProber("exit_geo", 1800),
        ]
    )
    last_seen = {
        # delay ran 30s ago — must be skipped (well under the 60s + 5% slack)
        "delay": now - timedelta(seconds=30),
        # exit_geo ran 1799s ago — within slack (5% capped at 5s) so it IS due
        "exit_geo": now - timedelta(seconds=1799),
    }
    due = registry.due(None, last_seen, now=now)
    assert {prober.metric for prober in due} == {"exit_geo"}


def test_registry_due_zero_interval_always_runs():
    now = datetime(2026, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
    registry = ProbeRegistry([_GatedProber("ad_hoc", 0)])
    due = registry.due(None, last_seen={"ad_hoc": now}, now=now)
    assert [prober.metric for prober in due] == ["ad_hoc"]


def test_registry_due_normalises_naive_timestamps_as_utc():
    now = datetime(2026, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
    registry = ProbeRegistry([_GatedProber("delay", 60)])
    naive_recent = (now - timedelta(seconds=10)).replace(tzinfo=None)
    due = registry.due(None, last_seen={"delay": naive_recent}, now=now)
    assert due == []  # treated as recent UTC, must be skipped


def test_registry_due_respects_dimensions_filter():
    now = datetime(2026, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
    registry = ProbeRegistry(
        [
            _GatedProber("delay", 60),
            _GatedProber("exit_geo", 1800),
        ]
    )
    due = registry.due(["delay"], last_seen={}, now=now)
    assert [prober.metric for prober in due] == ["delay"]


@pytest.mark.asyncio
async def test_last_metric_timestamps_returns_only_successful_results():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:")
    session_factory = async_sessionmaker(engine, expire_on_commit=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    try:
        async with session_factory() as session:
            node = Node(name="node-a", status="available")
            session.add(node)
            await session.flush()
            old = datetime(2026, 1, 1, 11, 0, 0, tzinfo=timezone.utc)
            mid = datetime(2026, 1, 1, 11, 30, 0, tzinfo=timezone.utc)
            new = datetime(2026, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
            await repository.add_probe_result(
                session, node, metric="delay", target="t",
                latency_ms=100.0, success=True, error=None, at=old,
            )
            await repository.add_probe_result(
                session, node, metric="delay", target="t",
                latency_ms=110.0, success=True, error=None, at=new,
            )
            # A failed result must not move the timestamp forward.
            await repository.add_probe_result(
                session, node, metric="delay", target="t",
                latency_ms=None, success=False, error="boom",
                at=new + timedelta(minutes=1),
            )
            await repository.add_probe_result(
                session, node, metric="exit_geo", target="g",
                latency_ms=None, success=True, error=None, at=mid,
            )
            await session.commit()

            timestamps = await repository.last_metric_timestamps(session, node.id)
            assert set(timestamps.keys()) == {"delay", "exit_geo"}
            # SQLite returns naive datetimes; compare as naive UTC.
            assert timestamps["delay"].replace(tzinfo=timezone.utc) == new
            assert timestamps["exit_geo"].replace(tzinfo=timezone.utc) == mid
    finally:
        await engine.dispose()
