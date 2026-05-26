# AGENT.md

# Project

Proxy Node Quality Detection Platform

A distributed proxy node quality monitoring and scoring platform based on Mihomo (Clash Meta).

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

The platform exposes:

- REST API
- WebSocket live updates
- Prometheus metrics
- Grafana dashboards

The platform MUST support:

- high concurrency
- async workers
- low resource usage
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

## Every node must support:

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
- distributed scheduling
- worker assignment
- retry
- timeout

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

Use:

- SQLite initially
- abstract storage interface
- later support PostgreSQL

Tables:

- nodes
- test_results
- latency_history
- bandwidth_history
- unlock_status
- probe_agents

---

### 5. Metrics

Expose Prometheus metrics.

Examples:

- node_latency_ms
- node_packet_loss
- node_jitter
- node_availability
- node_bandwidth_mbps

---

### 6. Dashboard

Use Grafana.

Provide dashboard JSON examples.

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

# API Design

## REST API

### GET /nodes

List all nodes.

---

### GET /nodes/{id}

Node details.

---

### GET /nodes/{id}/history

Historical metrics.

---

### GET /stats

Global statistics.

---

### POST /test/{id}

Trigger immediate test.

---

### WebSocket

Provide real-time updates.

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

Optional.

If implemented:

- React
- Next.js
- Tailwind

---

# Code Requirements

## Style

- typed Python
- modular design
- service-oriented
- repository pattern

---

## Logging

Use structured logging.

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

Support hot reload.

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
│   ├── workers/
│   ├── probes/
│   ├── storage/
│   ├── services/
│   ├── models/
│   └── utils/
├── configs/
├── dashboards/
├── scripts/
├── tests/
├── docker/
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

Support:

- retry
- timeout
- dead node quarantine
- cooldown

---

# Future Features

Support future extensions:

- distributed probes
- global multi-region probes
- Telegram notifications
- WeCom notifications
- OpenAI availability tracking
- route tracing
- ASN blacklist
- risk ASN detection
- historical trend analysis
- anomaly detection

---

# Deployment

Support:

- Docker
- docker-compose
- systemd

Provide:

- example compose files
- production configs

---

# Important Rules

## DO NOT:

- switch Clash global mode repeatedly
- restart Clash frequently
- use blocking network requests
- rely only on ICMP ping

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

Implementation priority:

1. Clash API integration
2. Concurrent delay testing
3. Storage
4. REST API
5. Metrics
6. Dashboard
7. Advanced scoring
8. Distributed architecture
