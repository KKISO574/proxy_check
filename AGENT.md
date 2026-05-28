# AGENT.md

# Project

Proxy Node Quality Detection Platform

A proxy node quality monitoring platform based on Mihomo (Clash Meta).
The current `README.md` defines the delivered v1 scope. This file is the
long-term engineering blueprint and roadmap.

The system continuously tests all proxy nodes concurrently and records:

- latency
- RTT
- jitter
- packet loss
- TCP connect time
- TLS handshake time
- HTTP response time
- outbound IP
- ASN
- GEO location
- OpenAI availability
- Netflix unlock
- YouTube region
- bandwidth
- historical stability

The platform exposes now:

- REST API

Future platform phases add:

- WebSocket live updates
- Prometheus metrics
- Grafana dashboards

The platform MUST support:

- high concurrency
- async workers
- low resource usage

Long-term phases SHOULD support:

- distributed probe nodes

---

# Core Requirements

## Clash Core

Use:

- Mihomo (Clash Meta)

Enable:

```yaml
external-controller: 0.0.0.0:9090
secret: your_secret
```

The system MUST use Clash External Controller API.

DO NOT switch global selector repeatedly for testing.

Each node MUST be tested independently through:

```http
GET /proxies/{name}/delay
```

---

# Testing Requirements

## Current v1 probes

### 1. Delay test

Using Clash delay API:

```http
/proxies/{name}/delay
```

Target:

```text
https://cp.cloudflare.com/generate_204
```

Timeout configurable.

---

### 2. TCP connect latency

Test:

- 443
- 80

Targets:

- 1.1.1.1
- 8.8.8.8

---

## Roadmap probes

### 3. TLS handshake time

Measure TLS establish duration.

---

### 4. HTTP RTT

Measure full HTTP request latency.

---

### 5. Download speed test

Small file benchmark:

- 1MB
- 10MB

Must support cancellation and timeout.

---

### 6. Packet loss

Continuous ping-like testing.

Do NOT rely only on ICMP.

Prefer TCP/HTTP-based loss detection.

---

### 7. Jitter

Compute jitter based on historical RTT variance.

---

### 8. Outbound IP

Fetch:

```text
https://api.ip.sb/ip
```

and:

```text
https://ipapi.co/json
```

Record:

- IP
- ASN
- Country
- Region
- ISP

---

### 9. Streaming unlock

Support:

- Netflix
- Disney+
- YouTube Premium
- TikTok
- OpenAI

---

### 10. DNS leak test

Detect whether outbound DNS leaks.

---

# Architecture

## Components

### 1. Mihomo Core

Responsible only for proxy transport.

---

### 2. Scheduler

Responsible for:

- periodic testing
- retry
- timeout

Future distributed phases add:

- distributed scheduling
- worker assignment

---

### 3. Probe Workers

Async workers performing actual tests.

Must support:

- asyncio
- concurrency limits
- cancellation
- timeout

---

### 4. Storage Layer

Current storage:

- SQLite initially
- abstract storage interface

PostgreSQL is not planned until a real production need appears.

Current tables:

- nodes
- monitor_tasks
- probe_results

Roadmap tables:

- node_meta
- probe_agents

---

### 5. Metrics

Metrics are currently exposed through REST API responses. Prometheus is a
v3 observability task.

Examples:

- node_latency_ms
- node_packet_loss
- node_jitter
- node_availability
- node_bandwidth_mbps

---

### 6. Dashboard

Current dashboard: React + Vite + TypeScript + Recharts.

Grafana dashboard JSON examples are a v3 observability task.

---

# Performance Requirements

Support:

- 1000+ nodes
- 100 concurrent tests
- low CPU usage
- low memory usage

Must use:

- asyncio
- aiohttp

Avoid:

- threading
- blocking requests

---

# Implementation Status

This section reflects what currently lives on `master`. Use it as the
single source of truth before picking up any task — anything not listed
as **Done** belongs in the Roadmap below.

## Done (v1.x)

### Backend

- **Mihomo integration** (`app/probes/mihomo.py`)
  - Subprocess managed under `MihomoManager`; stop / start swap with
    `_wait_ready()` polling `GET /version` 30 × 100 ms before returning
  - `MihomoClient.delay()` calls `/proxies/{name}/delay` against the
    External Controller API
  - Listener port map passed in explicitly (no formula), so DB-stored
    ports match the runtime YAML
- **Probes**
  - `delay` via Clash API → target `https://cp.cloudflare.com/generate_204`
  - `tcping` via per-node `mixed` listener → SOCKS5 CONNECT to
    `1.1.1.1:443/80` and `8.8.8.8:443/80`
- **Multi-config tasks**
  - `MonitorTask` row per imported Clash YAML URL (`http`/`https` only)
  - Task CRUD/refresh/run REST endpoints (see API Design)
  - Per-task `interval_seconds`; scheduler runs only due tasks
  - Cached YAML lives at `mihomo.imported_config_dir/task-{id}.yaml`;
    URL is re-downloaded only on create / URL edit / manual refresh
  - Same node name across tasks is **not** merged
- **Listener port allocation** (`app/services/port_allocator.py`)
  - Gap-finding allocator over `[listener_port_start, listener_port_max]`
  - Stable existing assignments; cross-task port collisions impossible
  - Range exhaustion raises `MihomoUnavailable`
- **Config import** (`app/services/config_import.py`)
  - URL fetch under `probe.import_timeout_ms` (default 30 s, separate
    from `probe.timeout_ms`)
  - YAML validated before write; atomic file replace
- **Scheduler** (`app/scheduler/runner.py`)
  - Single asyncio loop, 5 s polling granularity, runs `task.next_run_at`
    that has fallen due
- **Storage**
  - SQLite via SQLAlchemy 2.0 async + aiosqlite
  - Tables: `monitor_tasks`, `nodes` (FK→tasks, `UniqueConstraint(task_id, name)`),
    `probe_results`
  - Migration: legacy global-unique `nodes` table is rebuilt; missing
    `task_id` column is added; default task seeded for orphan rows
- **REST API** (`app/api/routes.py`)
  - `/api/tasks` CRUD/refresh/run
  - `/api/nodes`, `/api/nodes/{id}`, `/api/nodes/{id}/history`
  - `/api/stats`, `/api/tests/run`
- **Retention**: probe results older than `probe.retention_days`
  (default 30) are pruned each run

### Frontend

- React 18 + Vite + TypeScript + Recharts UI under `frontend/`
- Multi-task sidebar with import/edit/refresh/run/delete
- Node table with delay & tcping columns, status badges
- Detail panel: delay & tcping line charts, recent error log
- Polled refresh, selected task preserved across polls

### DevOps & Tooling

- Docker multi-stage build (Node 22 builder → Python 3.12 slim runtime)
- `docker-compose.yml`; `APP_PORT` host-port override
- `scripts/download_mihomo.py` for per-platform binary fetch
- `pytest` suite covering tasks, scheduler, port allocator, mihomo
  health, migration, clash config, tcping

---

# Roadmap

What's left vs. the original blueprint above. Keep detailed execution
plans under `docs/plans/` before implementation; this section is the
high-level map.

## Direction

- **v1.x Done**: multi-config URL import, monitor tasks, Mihomo process
  management, `delay` + `tcping`, SQLite history, React dashboard,
  Docker deployment, and basic REST API.
- **v2 Next**: metric abstraction, more probe dimensions, exit
  IP/ASN/GEO enrichment, and frontend metric tabs.
- **v3+ Later**: scoring, Prometheus/Grafana, WebSocket alerts,
  distributed probe agents, and advanced risk/anomaly detection.

## Execution priority

- **P0 Done**: v2.0 metric model refactor (`ProbeResult.value/data`,
  `MetricSummary`, `Prober` registry).
- **P1 Done**: v2.1 low-risk metrics: `tls_handshake`, `http_rtt`,
  `jitter`, `packet_loss`. Bandwidth tests remain delayed until
  timeout and traffic controls are clear.
- **P2 Partial Done**: v2.2 node enrichment: exit IP, ASN, country,
  region, ISP via `NodeMeta`.
- **P3 Partial Done**: v2.3 frontend: metric tabs, NodeMeta card, and
  country/ASN columns and filters.
- **P4**: v3 observability: score, Prometheus, Grafana examples,
  structured logging.
- **P5**: v4/v5 realtime alerts and distributed probes after the
  single-node metric model is stable.

## Do not start with

- Base64 subscription parsing or `proxy-providers` expansion.
- Netflix/Disney/YouTube unlock and DNS leak before `NodeMeta` and
  generic probers exist.
- PostgreSQL or distributed probe agents before the single-node model is
  stable.

## v2 — Probe dimensions + node enrichment (next)

Two main lines, requiring a small core refactor first.

### v2.0 Core abstractions (done)

- `ProbeResult` schema: add `value: float | None` and `data: str | None`
  (JSON) for non-latency metrics (mbps, percentages, sample arrays);
  keep `latency_ms` during transition
- `NodeMeta` table for non-time-series fields (one-to-one with `Node`):
  `exit_ip / asn / country / region / isp / netflix_unlock /
  disney_unlock / openai_unlock / youtube_unlock / dns_leak`
- `Prober` Protocol + registry (`app/probes/base.py`,
  `app/probes/registry.py`); each prober declares its own
  `metric` and `interval_seconds`
- Refactor `_probe_node` to iterate the registry instead of hard-coding
  delay + tcping
- `nodes_with_latest_metrics` parameterized by metric list; API +
  frontend types switch to `metrics: dict[str, MetricSummary]`

### v2.1 New probe dimensions (partial done)

| Metric | Source | Interval |
|---|---|---|
| `tls_handshake` | SOCKS5 + `ssl.create_default_context()` to `cp.cloudflare.com:443` | 60 s |
| `http_rtt` | SOCKS5 + GET `https://www.gstatic.com/generate_204` | 60 s |
| `jitter` | derived: stddev of last 20 `delay` samples | derived |
| `packet_loss` | 20 × tcping series, success rate as percentage | 5 min |
| `bandwidth_dl_1mb` / `bandwidth_dl_10mb` | SOCKS5 download from `speed.cloudflare.com/__down` | 30 min |

Done: `tls_handshake`, `http_rtt`, `jitter`, and `packet_loss`.
Deferred: `bandwidth_dl_1mb` / `bandwidth_dl_10mb`.

Config: `probe.dimensions: list[str]` gates which probers are registered.

### v2.2 Node enrichment (partial done)

These probers write to `NodeMeta` (upsert) instead of `ProbeResult`.

| Field group | Source | Interval |
|---|---|---|
| `exit_ip / asn / country / region / isp` | `https://ipapi.co/json` (fallback `api.ip.sb/geoip`) | 30 min |
| `netflix_unlock` | Netflix title page response analysis | 1 h |
| `disney_unlock` | Disney+ GraphQL device endpoint | 1 h |
| `openai_unlock` | `chat.openai.com/cdn-cgi/trace` `loc=` field | 1 h |
| `youtube_region` | YouTube `/red` endpoint region detection | 1 h |
| `dns_leak` | dnsleaktest results-json | 1 h |

Done: `exit_ip / asn / country / region / isp` via `exit_geo`.
Deferred: streaming unlock and DNS leak probes.

### v2.3 Frontend

- Done: NodeMeta card, metric tabs auto-generated from `metrics` dict,
  country/ASN columns, and filters by country/ASN.
- Next: country flag display, unlock badges (N / D / O / Y), and filters
  by unlock status after unlock probes exist.

## v3 — Observability & scoring

- 0–100 weighted node score (latency + loss + jitter + availability +
  bandwidth + unlock weights)
- Prometheus `/metrics` endpoint (`node_latency_ms`,
  `node_packet_loss`, `node_jitter`, `node_availability`,
  `node_bandwidth_mbps`)
- Grafana dashboard JSON examples
- Replace `logging` with `structlog`

## v4 — Realtime & alerting

- WebSocket `/api/ws` push for status changes
- Telegram / WeCom webhook on `available → down` transitions

## v5 — Distributed probes

- New `probe_agents` table
- agent ↔ controller protocol (gRPC or HTTP)
- Multi-region scheduling and result aggregation

## v6 — Advanced detection

- Route tracing
- ASN blacklist / risk ASN classification
- Anomaly detection on historical trends

## Explicitly out of scope (no plan to add)

- Base64 subscription parsing
- `proxy-providers` expansion
- PostgreSQL backend (storage interface stays abstract but no driver
  planned until a real need surfaces)

---

# API Design

## REST API

### Multi-Config Tasks

The platform groups Clash/Mihomo configurations into **monitor tasks**. Each
task owns:

- one source URL (`http`/`https` only)
- one cached YAML file under `mihomo.imported_config_dir`
- its own `interval_seconds` for scheduled detection
- its own listener port range (assigned out of `mihomo.listener_port_start`
  ~ `listener_port_max`, picked via gap-finding so tasks never collide)
- isolated nodes and history (same node name across tasks is NOT merged)

Re-downloading the source URL only happens on task creation, URL edit, or
manual `POST /api/tasks/{id}/refresh`. Probe rounds reuse the cached YAML.

### GET /api/tasks

List all monitor tasks, including `node_count` and last-run status.

### POST /api/tasks

Create a task by importing a Clash YAML URL. Body:

```json
{
  "name": "main",
  "source_url": "https://example.com/clash.yaml",
  "interval_seconds": 60,
  "enabled": true
}
```

### PATCH /api/tasks/{id}

Edit `name`, `source_url`, `enabled`, or `interval_seconds`. Changing
`source_url` triggers a refresh.

### DELETE /api/tasks/{id}

Delete the task, its nodes, and all probe history.

### POST /api/tasks/{id}/refresh

Re-download the source URL and resync the node list (deduplicated by name
within the task).

### POST /api/tasks/{id}/run

Trigger an immediate detection round for one task.

### GET /api/nodes

List all nodes. Pass `?task_id={id}` to scope the list to one monitor task.

---

### GET /api/nodes/{id}

Node details.

---

### GET /api/nodes/{id}/history

Historical metrics. Use `?metric=delay|tcping&range=1h|6h|24h|7d|30d`.

---

### GET /api/stats

Global statistics. Pass `?task_id={id}` for per-task aggregates.

---

### POST /api/tests/run

Trigger an immediate detection round for the legacy local-config mode.

---

### WebSocket

Future v4 task: provide real-time updates through `/api/ws`.

---

# Node Scoring

Every node must have dynamic score.

Suggested formula:

```text
score =
latency_weight +
packet_loss_weight +
jitter_weight +
availability_weight +
bandwidth_weight +
unlock_weight
```

Score range:

```text
0 ~ 100
```

---

# Suggested Stack

## Backend

Python 3.12+

Framework:

- FastAPI

Libraries:

- aiohttp
- asyncio
- sqlalchemy
- pydantic
- uvicorn

---

## Frontend

Current implementation:

- React 18
- Vite
- TypeScript
- Recharts

---

# Code Requirements

## Style

- typed Python
- modular design
- service-oriented
- repository pattern

---

## Logging

Current implementation uses standard Python logging. Structured logging is
a v3 observability task.

Recommended:

```python
structlog
```

---

## Config

Use:

```yaml
config.yaml
```

Config hot reload is not part of v1; add it only if a future operational
need appears.

---

## Secrets

Do NOT hardcode:

- API secrets
- Clash secret
- tokens

Use env vars.

---

# Directory Structure

```text
project/
├── app/
│   ├── api/
│   ├── core/
│   ├── scheduler/
│   ├── probes/
│   ├── storage/
│   └── services/
├── configs/
├── frontend/
├── scripts/
├── tests/
├── data/       # ignored runtime SQLite data
├── runtime/    # ignored Mihomo binaries/config cache
├── Dockerfile
├── docker-compose.yml
└── AGENT.md
```

---

# Probe Strategy

Testing must be:

- concurrent
- isolated
- timeout-controlled

Never block entire testing loop due to single node failure.

Use semaphore.

Example:

```python
asyncio.Semaphore(50)
```

---

# Failure Handling

Current support:

- timeout
- single-node failure isolation
- historical success/error persistence

Future support:

- retry policy
- dead node quarantine
- cooldown

---

# Deployment

Current support:

- Docker
- docker-compose

Future support:

- systemd

Provide:

- example compose files
- production configs when hardening for a real deployment target

---

# Important Rules

## DO NOT:

- switch Clash global mode repeatedly
- restart Clash frequently
- use blocking network requests
- rely only on ICMP ping
- exceed the configured listener port range; per-task node count is
  bounded by `mihomo.listener_port_max - listener_port_start` shared
  across all tasks (default ~45000 slots). Allocations are picked via
  gap-finding rather than `start + task_id * 1000 + index`, so node
  count is the only practical limit, not task ID.

---

## MUST:

- use Clash delay API
- support async concurrency
- support metrics
- support historical persistence
- support extensibility

---

# Testing Targets

Default targets:

```text
https://cp.cloudflare.com/generate_204
https://www.gstatic.com/generate_204
https://captive.apple.com
```

---

# Priority

The original implementation order was the v1 plan; the current
phasing lives in **Roadmap** above (v2 → v3 → v4 → v5 → v6).
