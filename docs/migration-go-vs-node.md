# 后端语言迁移评估：Go vs Node.js

## 1. 现状与动机

### 当前技术栈

| 层 | 技术 |
|---|---|
| Web 框架 | FastAPI (Starlette) |
| 异步运行时 | asyncio (CPython 3.12) |
| HTTP 客户端 | aiohttp |
| ORM / DB | SQLAlchemy 2.0 async + aiosqlite (SQLite) |
| 代理核心 | Mihomo (Clash Meta) — subprocess 管理 |
| 前端 | React + TypeScript (Vite) |
| 部署 | Docker (python:3.12-slim base) |

后端 Python 代码约 3,100 行（不含测试），测试约 1,900 行。

### 痛点

1. **Mihomo 只能 subprocess**：需要健康检查轮询、stdout/stderr 消费、
   进程生命周期管理。崩溃恢复逻辑脆弱。
2. **GIL + asyncio 在高并发探测时的抖动**：1000+ 节点并发 SOCKS5
   连接时，事件循环调度延迟会引入额外 jitter，影响 delay 测量精度。
3. **subprocess stdout 缓冲区风险**：Mihomo 日志未被消费，长时间运行
   可能导致 pipe buffer 满阻塞子进程。
4. **镜像体积**：python:3.12-slim + pip 依赖 → 约 300 MB。
5. **依赖链长**：aiohttp / SQLAlchemy / uvicorn / pydantic 各自有
   C 扩展编译需求，交叉编译困难。

---

## 2. 候选 1：Go

### 优势

- **与 Mihomo 同语言**（`github.com/metacubex/mihomo`），可作为库
  直接 import，消除 subprocess 管理的全部复杂度。
- HTTP / SOCKS5 / TLS / DNS 标准库齐全，`net/http` + `chi` 或
  `gin` 可替代 FastAPI。
- `sqlx` + `modernc.org/sqlite`（纯 Go SQLite）替代 SQLAlchemy +
  aiosqlite，无 CGO 依赖。
- 单二进制部署，Docker 镜像 scratch/distroless 约 20-30 MB。
- goroutine + `context.WithTimeout` 并发模型比 asyncio 更稳定，
  无 GIL 瓶颈。
- 交叉编译简单（`GOOS=linux GOARCH=arm64 go build`），利于 edge 部署。

### 劣势

- 团队若无 Go 经验，上手成本中等（2-3 周熟悉期）。
- Mihomo 内嵌 API 不保证稳定，需 vendor 并跟进上游 breaking change。
- Go 的 ORM 生态不如 Python 成熟（`sqlx` 手写 SQL，`ent` 代码生成）。
- 错误处理样板代码多，总行数预估比 Python 多 40-60%。

---

## 3. 候选 2：Node.js / TypeScript

### 优势

- 与前端共用 TypeScript 类型定义（tRPC 或 OpenAPI codegen）。
- `Fastify` + `Prisma` 替代 FastAPI + SQLAlchemy，开发体验好。
- 团队已熟悉 TypeScript，学习成本最低。
- npm 生态丰富，SOCKS5 库（`socks`）、TLS 库成熟。

### 劣势

- **Mihomo 仍只能 subprocess**（Node 无法 import Go 库），核心痛点
  未解决。
- 单线程事件循环与 Python asyncio 本质相同，高并发探测无质变。
- 部署镜像 node:20-slim 约 150 MB，优于 Python 但远不如 Go。
- Prisma 对 SQLite 支持完善但运行时需要 Query Engine 二进制。

---

## 4. 维度对比表

| 维度 | Python (现状) | Go | Node.js |
|---|---|---|---|
| 镜像大小 | ~300 MB | ~25 MB | ~150 MB |
| Mihomo 集成 | subprocess | 内嵌库 import | subprocess |
| 并发模型 | asyncio (GIL) | goroutine (真并行) | event loop (单线程) |
| 1000 节点探测稳定性 | 中 | 高 | 中 |
| DB 生态 | SQLAlchemy (优秀) | sqlx/ent (中等) | Prisma (优秀) |
| 团队学习成本 | 0 | 中 | 低 |
| 前后端类型同源 | 否 | 否 | 是 |
| 后端代码量 (预估) | 3,100 行 | ~5,000 行 | ~3,500 行 |
| 重写工作量 | n/a | 3-4 周 | 2-3 周 |
| 交叉编译 | 困难 | 简单 | 中等 |
| subprocess 消除 | 否 | 是 | 否 |

---

## 5. 推荐

### 保留 Python（当前规模 < 500 节点）

如果不追求 1000+ 节点低延迟，保留 Python 是最经济的选择。
后续优化方向：

- 切 `uvloop`（Linux/Mac）提升事件循环吞吐
- 将 `_probe_node` 内的 aiohttp.ClientSession 改为 long-lived 复用
- Mihomo 走 supervisor 模式：独立进程托管 + 自动重启 + 日志消费
- 考虑 `ProcessPoolExecutor` 分担 CPU 密集型计算（如大量 jitter 统计）

### 迁移到 Go（目标 1000+ 节点或分布式探针）

- 内嵌 Mihomo 彻底消除 subprocess 健康检查脆弱性
- 单二进制极简部署，利于 v5 分布式探针的 worker agent 分发
- goroutine 并发裕度足够支撑 v5 的 agent ↔ controller 长连接
- 性能天花板远高于 Python/Node.js

### 不推荐 Node.js

Node.js 能解决的痛点（类型同源、开发体验）Python 也能通过 OpenAPI
codegen 部分解决；而 Node.js 解决不了的核心痛点（Mihomo 内嵌、真并行）
Go 能解决。迁移到 Node.js 的 ROI 不足以证明重写成本。

---

## 6. 迁移路径（如选 Go）

| Phase | 周期 | 内容 |
|---|---|---|
| 1 | 1 周 | 搭 Go 服务壳：chi router、SQLite 连接、复刻全部 REST API 端点。DB schema 共用现有 SQLite 文件，零迁移。 |
| 2 | 1 周 | 实现 Prober 接口，移植 delay / tcping / tls_handshake / http_rtt 四个核心探测器。 |
| 3 | 1 周 | 内嵌 Mihomo（或先 Go-managed subprocess 作为过渡），实现节点生命周期管理。 |
| 4 | 0.5 周 | 前端只改 `BASE_URL`，保留全部 API 契约不变。端到端验证。 |
| 5 | 0.5 周 | 双跑对照（Python + Go 同时运行，比对探测结果），灰度切换。 |

总计约 4 周。Phase 3 风险最高（Mihomo 内部 API 稳定性），可降级为
Go-managed subprocess 先上线，内嵌作为后续优化。

---

## 7. 决策门槛

满足以下**任一**条件即可启动迁移：

1. 节点数稳定超过 500，单轮探测延迟 P99 > 30s
2. 计划启动 v5 分布式探针，需要轻量 agent 二进制（< 50 MB）
3. Docker 镜像大小成为部署痛点（如部署到资源受限的 edge node）
4. Mihomo subprocess 崩溃恢复问题频繁影响可用性

**未达到上述条件前保持 Python**，按 §5 的优化方向迭代即可。
迁移是一个可选的性能/架构升级，不是紧急修复。
