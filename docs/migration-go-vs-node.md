# 后端语言迁移评估：Go vs Node.js

## 1. 现状与动机

### 迁移前技术栈

| 层 | 技术 |
|---|---|
| Web 框架 | FastAPI (Starlette) |
| 异步运行时 | asyncio (CPython 3.12) |
| HTTP 客户端 | aiohttp |
| ORM / DB | SQLAlchemy 2.0 async + aiosqlite (SQLite) |
| 代理核心 | Mihomo (Clash Meta) — subprocess 管理 |
| 前端 | React + TypeScript (Vite) |
| 部署 | Docker (python:3.12-slim base) |

迁移前后端 Python 代码约 3,100 行（不含测试），测试约 1,900 行。
当前主线已经迁移到 Go，Python/FastAPI 后端代码已从仓库移除。

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
6. **高级探测后端趋向 Go 生态**：后续计划接入
   `MiaoMagic/miaospeed` 做 DNS leak、解锁、跑量/带宽测试。Go 后端
   更适合统一管理 Mihomo 与 MiaoSpeed 的生命周期、并发和发布形态。

---

## 2. 候选 1：Go

### 优势

- **与 Mihomo 同语言**（`github.com/metacubex/mihomo`），未来可评估
  作为库直接 import，消除 subprocess 管理复杂度。
- **与 MiaoSpeed 同语言**，便于后续统一管理高级探测 sidecar、
  WebSocket 通道和结果适配。
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

### 迁移到 Go（目标 1000+ 节点、高级探测或分布式探针）

- 内嵌 Mihomo 彻底消除 subprocess 健康检查脆弱性
- Go-managed MiaoSpeed sidecar 降低 DNS leak / 解锁 / 跑量测试的
  适配成本
- 单二进制极简部署，利于 v5 分布式探针的 worker agent 分发
- goroutine 并发裕度足够支撑 v5 的 agent ↔ controller 长连接
- 性能天花板远高于 Python/Node.js

### 不推荐 Node.js

Node.js 能解决的痛点（类型同源、开发体验）Python 也能通过 OpenAPI
codegen 部分解决；而 Node.js 解决不了的核心痛点（Mihomo 内嵌、真并行）
Go 能解决。迁移到 Node.js 的 ROI 不足以证明重写成本。

---

## 6. 迁移路径（已选 Go）

当前决策：项目主线直接开启 Go 后端转型。Go 后端现在已经覆盖 API、
SQLite、调度、核心探测器、Mihomo 生命周期、静态页面托管和第二轮
上线硬化；Python/FastAPI 后端已下线。MiaoSpeed 已在 Go 侧
补到请求构造、Challenge 签名、WebSocket client、按脚本 Key 的 frame
归一化、sidecar 默认启动参数、带宽 prober、基于 `TEST_SCRIPT` 的 DNS
leak / 解锁 prober 和任务级高级探测开关；最小真实 sidecar 联调已覆盖
签名 `TEST_PING_CONN`、`TEST_SCRIPT`、`Vendor=Clash` HTTP 节点 payload
和 `SPEED_*` 带宽矩阵，下一步验证生产 DNS/解锁脚本。

| Phase | 周期 | 内容 |
|---|---|---|
| 1 | 已完成 | 搭 Go 服务壳：SQLite schema 初始化、REST API 端点、Prometheus、React 静态页面托管。 |
| 2 | 已完成 | 实现 Prober 接口，移植 `delay` / `tcping` / `tls_handshake` / `http_rtt` / `packet_loss` / `jitter` / `exit_geo`。 |
| 3 | 已完成 | Go-managed Mihomo subprocess，生成 runtime config 和每节点 mixed listener；runtime 签名用于检测同路径配置/listener 变化并触发重启。 |
| 3.5 | 已完成 | Go 上线硬化：配置 URL SSRF guard、实际连接地址复查、刷新失败状态写入、创建失败清理、history 404 parity、Mihomo 退出观察。 |
| 4 | 进行中 | Go-managed MiaoSpeed sidecar；当前已有 Go 请求构造、Challenge 签名、WebSocket client、响应 frame 归一化、sidecar 生命周期、正式 release 已验证的 `server -token ... -bind ...` 启动方式、`miaospeed_bandwidth`、基于 `TEST_SCRIPT` 的 `miaospeed_dns_leak` / `miaospeed_unlock` prober 和任务级高级探测开关；最小真实 sidecar 联调已覆盖 signed ping/script、`Vendor=Clash` HTTP 节点和 `SPEED_*` 带宽矩阵，下一步补生产 DNS/解锁脚本联调。 |
| 5 | 下一步 | 端到端验证 Docker 与远端部署，保留全部 API 契约不变。 |
| 6 | 已取消 | Python 后端已移除，不再做 Python + Go 双跑；后续以 Go API、SQLite 数据和前端行为回归为验收标准。 |

真实 MiaoSpeed 联调入口已经放在
`backend/internal/miaospeed/integration_test.go`，默认跳过；需要提供
`PROXY_CHECK_MIAOSPEED_INTEGRATION=1`、`MIAOSPEED_BIN`、`MIAOSPEED_TOKEN`
和可选 `MIAOSPEED_BUILD_TOKENS`。当前已经用临时补齐 embed 资源后本机构建
的 MiaoSpeed 4.3.9-Core 验证了签名 `TEST_PING_CONN`、`TEST_SCRIPT`、
`Vendor=Clash` HTTP 节点 payload 和 `SPEED_*` 带宽矩阵链路。公开上游仓库
直接 `go build` 会缺少预构建嵌入资源，正式验证仍应使用上游发布二进制或完成
对应 prebuild 资源准备。

总计约 5 周。Phase 3/4 风险最高：Mihomo 内部 API 稳定性和
MiaoSpeed AGPLv3 / WebSocket 协议适配都需要先做 spike。Go-managed
subprocess/sidecar 是第一阶段上线形态，内嵌作为后续优化。

---

## 7. 原决策门槛（已被用户决策覆盖）

原先建议满足以下**任一**条件再启动迁移：

1. 节点数稳定超过 500，单轮探测延迟 P99 > 30s
2. 计划启动 v5 分布式探针，需要轻量 agent 二进制（< 50 MB）
3. Docker 镜像大小成为部署痛点（如部署到资源受限的 edge node）
4. Mihomo subprocess 崩溃恢复问题频繁影响可用性
5. MiaoSpeed 接入成为核心能力，Python 侧维护多个长生命周期
   sidecar/WebSocket 通道开始影响可靠性

现在用户已明确要求直接开启 Go 语言转型，因此该门槛只保留为历史
判断依据，不再阻止执行。后续验收以 Go 主线为准：API 契约兼容、
SQLite schema 兼容、调度与 prober parity、Mihomo/MiaoSpeed 生命周期稳定、
Docker/远端部署可回归。
