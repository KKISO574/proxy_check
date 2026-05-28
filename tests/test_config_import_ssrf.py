"""Unit tests for the SSRF defense in ``app.services.config_import``.

The sandbox forbids real ``socket.bind`` calls, and DNS resolution may not
behave consistently across CI environments — so we monkeypatch
``socket.getaddrinfo`` to return canned IPs and assert that the helper
``_resolve_and_validate_host`` rejects every private/internal address class
that an attacker might use to reach cloud metadata or internal services
through a user-supplied subscription URL.
"""

from __future__ import annotations

import socket

import pytest

from app.services import config_import
from app.services.config_import import (
    ConfigImportError,
    ConfigImportService,
    _resolve_and_validate_host,
)
from app.core.config import Settings


def _fake_getaddrinfo(addresses: list[str]):
    """Build a ``getaddrinfo`` stub that always returns the given addresses.

    Each entry is shaped like the real ``getaddrinfo`` 5-tuple so the
    SUT's ``info[4][0]`` extraction continues to work.
    """

    def _stub(host, port, *args, **kwargs):  # noqa: ARG001
        results = []
        for ip in addresses:
            family = socket.AF_INET6 if ":" in ip else socket.AF_INET
            sockaddr: tuple = (ip, 0, 0, 0) if family == socket.AF_INET6 else (ip, 0)
            results.append((family, socket.SOCK_STREAM, 0, "", sockaddr))
        return results

    return _stub


@pytest.mark.parametrize(
    "blocked_ip",
    [
        "127.0.0.1",          # loopback
        "10.0.0.1",           # RFC1918 private
        "172.16.0.1",         # RFC1918 private
        "192.168.1.1",        # RFC1918 private
        "169.254.169.254",    # link-local + cloud metadata endpoint
        "::1",                # IPv6 loopback
        "fe80::1",            # IPv6 link-local
        "fc00::1",            # IPv6 unique-local (private)
        "240.0.0.1",          # IPv4 reserved (240/4)
    ],
)
def test_resolve_and_validate_host_blocks_private_addresses(monkeypatch, blocked_ip):
    monkeypatch.setattr(
        config_import.socket,
        "getaddrinfo",
        _fake_getaddrinfo([blocked_ip]),
    )
    with pytest.raises(ConfigImportError, match="private/internal"):
        _resolve_and_validate_host("attacker.example.com")


def test_resolve_and_validate_host_rejects_when_any_address_is_private(monkeypatch):
    """Hosts that resolve to a mix of public + private must be rejected."""

    monkeypatch.setattr(
        config_import.socket,
        "getaddrinfo",
        _fake_getaddrinfo(["1.1.1.1", "10.0.0.1"]),
    )
    with pytest.raises(ConfigImportError, match="private/internal"):
        _resolve_and_validate_host("mixed.example.com")


def test_resolve_and_validate_host_accepts_public_addresses(monkeypatch):
    monkeypatch.setattr(
        config_import.socket,
        "getaddrinfo",
        _fake_getaddrinfo(["1.1.1.1", "2606:4700:4700::1111"]),
    )
    addresses = _resolve_and_validate_host("public.example.com")
    assert addresses == ["1.1.1.1", "2606:4700:4700::1111"]


def test_resolve_and_validate_host_translates_dns_failure(monkeypatch):
    def _boom(*args, **kwargs):  # noqa: ARG001
        raise socket.gaierror("Name or service not known")

    monkeypatch.setattr(config_import.socket, "getaddrinfo", _boom)
    with pytest.raises(ConfigImportError, match="failed to resolve"):
        _resolve_and_validate_host("does-not-exist.example.invalid")


def test_resolve_and_validate_host_rejects_empty_host():
    with pytest.raises(ConfigImportError, match="host is required"):
        _resolve_and_validate_host("")


@pytest.mark.asyncio
async def test_fetch_url_refuses_private_target_before_opening_socket(monkeypatch):
    """``fetch_url`` must short-circuit before any network IO happens."""

    monkeypatch.setattr(
        config_import.socket,
        "getaddrinfo",
        _fake_getaddrinfo(["169.254.169.254"]),
    )

    class _Boom:
        def __init__(self, *args, **kwargs):
            raise AssertionError(
                "aiohttp.ClientSession must not be constructed once SSRF guard fires"
            )

    monkeypatch.setattr(config_import.aiohttp, "ClientSession", _Boom)

    service = ConfigImportService(Settings())
    with pytest.raises(ConfigImportError, match="private/internal"):
        await service.fetch_url("http://metadata.example.com/latest/meta-data/")
