"""Exit IP / geolocation enrichment probe.

Fetches the node's apparent exit IP and ASN/country/region/ISP metadata
through the Mihomo SOCKS5 listener. Tries ``ipapi.co`` first and falls
back to ``api.ip.sb`` when the primary errors out (commonly rate
limiting). The JSON helper is resolved via ``app.probes.builtin`` so
tests can monkeypatch ``app.probes.builtin.socks5_http_get_json``.

The two providers expose overlapping but differently-named fields;
``_normalize_geo`` collapses them into the canonical
``(exit_ip, asn, country, region, isp)`` tuple persisted on
``NodeMeta`` via ``repository.upsert_node_meta``.
"""

from __future__ import annotations

import json

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.probes import builtin
from app.probes.base import ProbeContext, ProbeOutcome


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
        return await builtin.socks5_http_get_json(
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
