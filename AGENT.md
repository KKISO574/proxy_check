# Proxy Check 后续开发计划

`README.md` 记录当前可运行版本；本文件记录工程架构、已完成范围和后续路线。
当前主线后端已经切到 Go，Python/FastAPI 后端与 pytest 体系已下线。

## 当前架构

```text
proxy_check/
├── backend/                     # Go 后端主线
│   ├── cmd/proxy-check/         # 服务入口
│   └── internal/
│       ├── api/                 # REST API、Prometheus、静态资源托管
│       ├── clash/               # Clash/Mihomo YAML 解析
│       ├── config/              # YAML 配置加载
│       ├── miaospeed/           # MiaoSpeed WebSocket/sidecar 适配
│       ├── probe/               # Mihomo 管理、探测器、评分
│       ├── scheduler/           # 任务调度
│       └── storage/             # SQLite schema 与 repository
├── frontend/                    # React + Vite + TypeScript 前端
├── web/static/                  # 前端构建产物，Git 忽略，由 Go 托管
├── configs/                     # 示例配置与 Docker 配置
├── docs/grafana/                # Grafana 示例面板
├── scripts/download_mihomo.sh   # Mihomo 下载辅助脚本，不依赖 Python
├── scripts/download_miaospeed.sh # MiaoSpeed 下载辅助脚本，不依赖 Python
├── Dockerfile
└── docker-compose.yml
```

## 已完成

### v1 基础平台

- 多配置 URL 导入：每个 Clash/Mihomo YAML URL 对应一个监测任务。
- 任务模型：任务拥有独立节点、检测周期、启停状态和缓存配置文件。
- Mihomo 托管：Go 后端生成运行时配置，为每个节点分配独立 `mixed` listener。
- 基础探测：`delay` 和 `tcping`，不依赖 ICMP ping。
- SQLite 持久化：`monitor_tasks`、`nodes`、`probe_results`、`node_meta`。
- API：`/api/tasks`、`/api/nodes`、`/api/stats`、`/api/tests/run`、`/metrics`、
  `/api/tasks/{id}/miaospeed/run`、`/api/tasks/{id}/miaospeed/results`、`/api/miaospeed/catalog`。
- React 可视化：任务列表、节点表格、状态、历史折线图。
- Docker 部署：Node 22 构建前端，Go 构建后端，Debian slim runtime。

### v2 指标扩展

- Go prober registry 和统一指标模型。
- `tls_handshake`、`http_rtt`、`jitter`、`packet_loss`。
- `exit_geo`：出口 IP、ASN、国家、地区、ISP。
- 前端按指标动态生成图表 tab，并支持国家/ASN 过滤。
- 节点详情页已展示 DNS 泄漏、服务解锁和 MiaoSpeed 带宽结果。

### v3 可观测性

- 节点评分：`score`、`score_confidence`、`score_breakdown`。
- Prometheus `/metrics`。
- Grafana 示例：`docs/grafana/proxy-check-v3.json`。
- JSON 行日志，适合 Docker 和日志平台采集。

### v4 Go 主线与 MiaoSpeed

- Go 后端已覆盖 API、调度、存储、核心探测器、Mihomo 生命周期和静态页面托管。
- 配置导入具备 SSRF 防护，拒绝 localhost、内网、link-local、multicast 等地址。
- Mihomo runtime config/listener 变化会触发重建，子进程退出会被观察并清理。
- MiaoSpeed 适配已具备：
  - Challenge 签名与 WebSocket client
  - frame 归一化和按脚本 key 聚合结果
  - Go-managed sidecar 生命周期
  - `miaospeed_bandwidth`
  - 基于 `TEST_SCRIPT` 的 `miaospeed_dns_leak`
  - 基于 `TEST_SCRIPT` 的 `miaospeed_unlock`
  - 任务级高级探测开关，默认关闭
- 最小真实 sidecar 联调已经覆盖 signed ping/script、`Vendor=Clash` HTTP 节点 payload 和 `SPEED_*` 带宽矩阵。

## 当前约束

- 首版仍只解析 Clash/Mihomo YAML 顶层静态 `proxies`。
- 暂不做 Base64 订阅解析、`proxy-providers` 展开、PostgreSQL。
- MiaoSpeed 是 AGPLv3；分发修改版二进制、嵌入源码或深度派生前必须做许可证合规检查。
- DNS 泄漏和解锁检测必须走 MiaoSpeed 上游真实 `TEST_SCRIPT`，不要发明不存在的矩阵名。
- 高流量探测必须显式开启，不允许默认随 60 秒普通轮询运行。

## 后续路线

### P0 架构清理（已完成）

- 保持 Go 为唯一后端主线。
- Python/FastAPI 后端、pytest、requirements、pyproject 已移除。
- Mihomo/MiaoSpeed 下载辅助脚本已改为 Bash，仓库不再保留 Python 源码。
- 前端构建产物统一到 `web/static/`。
- 后续所有后端功能只在 `backend/` 中实现，入口统一为 `backend/cmd/proxy-check`。

### P0 AirportR/MiaoSpeed 生产化（当前最高优先级）

- MiaoSpeed 上游切换为 [AirportR/miaospeed](https://github.com/AirportR/miaospeed)，优先适配
  4.6.8 release；旧的 MiaoMagic 4.3.9-Core 仅作为历史验证记录。
- 已补 AirportR release 二进制下载脚本，默认写入 `runtime/bin/miaospeed`。
- Mihomo/MiaoSpeed 下载脚本支持 `--print-url`、`GITHUB_PROXY`、`DOWNLOAD_CONNECT_TIMEOUT`、
  `DOWNLOAD_MAX_TIME`、`DOWNLOAD_RETRY` 和 `DOWNLOAD_RETRY_DELAY`，用于网络受限环境预检、
  走代理下载、重试临时 SSL/连接错误或缩短失败等待。
- Docker 配置已按 Go 托管 sidecar 模式使用 `ws://127.0.0.1:8766`，避免默认指向不存在的外部服务。
- Go 托管 sidecar 现在要求 `miaospeed.enabled` 与 `miaospeed.manage_sidecar` 同时开启，避免全局关闭时仍启动进程。
- Go 托管 sidecar 默认执行 `server -token ... -bind ...`；AirportR 4.6.8 沿用该参数形态，
  且不按 `TOKEN`/`BIND` 环境变量自动启动；
  自定义 `miaospeed.args` 时按用户参数启动。
- 请求适配以 AirportR 4.6.x 为准：`Configs.ApiVersion` 默认 3，保留 `Vendor=Clash`
  参与 Challenge 签名，并支持后续上传测速字段。
- Prober factory 现在只有在 `miaospeed.enabled: true` 时才注册 `miaospeed_*` 维度。
- 已支持从文件加载 DNS/解锁脚本，推荐放在 `runtime/miaospeed/scripts/`。
- AirportR 预编译二进制的公开 build token 已作为默认配置写入；自行编译时用
  `MIAOSPEED_BUILD_TOKENS` 或 `miaospeed.build_tokens` 覆盖。
- 使用 AirportR 4.6.8 正式发布二进制验证生产 DNS leak 脚本。
- 验证 Netflix、Disney、OpenAI、YouTube 等解锁脚本输出。
- 将 MiaoSpeed 结果稳定写入 `probe_results` 与 `node_meta`。

### P1 MiaoSpeed 全量测试与结果图

目标对齐参考结果图，把 MiaoSpeed 从“少量高级探测补充”升级为可从前端手动控制的全量测试工作台：

- 已完成：MiaoSpeed full-test catalog、上传/下载矩阵 helper、full-test 配置、`miaospeed_full`
  prober 和 factory 注册。
- 已完成：`RunAdvancedTask` 服务层路径、MiaoSpeed 专用 API、结果矩阵响应结构、
  对应后端测试，以及 `/dns/` 的深度测试入口。
- 仍需完成：服务列选择、密集 MiaoSpeed full-test 结果矩阵页面、PNG 导出、AirportR 4.6.8 full matrix
  联调和结果截图示例文档。
- 流媒体和服务解锁必须走 MiaoSpeed `TEST_SCRIPT`，脚本从 `runtime/miaospeed/scripts/`
  或配置字段加载；未配置脚本显示 `未配置`，失败显示错误状态，不伪造结果。
- 默认服务列覆盖 Netflix、Disney+、YouTube、TikTok、OpenAI、Google、GitHub、Telegram、Spotify、Steam、
  Bilibili、Abema、DAZN、Hulu、Prime Video、HBO Max、Bahamut、BBC iPlayer、Claude、Gemini。
- 后续执行状态直接维护在本节；过期的独立计划书已删除，避免出现失效路线和重复信息。

### P2 前端面板重设计

视觉方向已优先切到 `ip.net.coffee` / `ip.net.coffee/ip/` / `ip.net.coffee/dns/`：

- 已完成：Net.Coffee 风格顶栏、暗色/亮色主题、居中 1000px 内容容器。
- `/` 保留节点监控台，但去掉左侧 Uptime Kuma 式侧栏，改为紧凑任务条 + 表格/详情面板。
- `/ip/` 已拆成独立 IP 画像页，展示出口 IP、评分、ASN/GEO/ISP、网络质量、DNS 和解锁信号。
- `/dns/` 已拆成独立 DNS 泄露检测页，包含功能卡、快速/深度检测按钮、结果表和折叠说明。
- clone-website 提取资料保存在 `docs/research/ip-net-coffee/`，参考和本地 QA 截图保存在
  `docs/design-references/`。
- 已用本地 Go 托管页面验证 `/`、`/ip/`、`/dns/` 桌面与 390px 移动宽度无横向溢出；
  `/dns/` 页面全局"导入"表单也已验证可打开。
- 仍需在真实订阅节点数据下细调密集结果矩阵和导出图。

### P3 评分模型升级

- 已加入 DNS 泄漏惩罚。
- 已加入 MiaoSpeed 带宽分项，`score_confidence` 封顶到 1.0。
- 已加入解锁能力作为可选轻量分项，同时继续作为页面 badge 展示。
- 后续可把带宽从固定阈值升级为任务内 percentile 分。
- 区分快速指标和重型指标的置信度权重。

### P4 运维增强

- WebSocket 实时状态推送。
- Telegram / 企业微信 webhook 告警。
- systemd 部署示例。
- 远端 Docker 部署回归脚本。

## API 约定

当前 API 继续保持：

- `GET /api/tasks`
- `POST /api/tasks`
- `PATCH /api/tasks/{id}`
- `DELETE /api/tasks/{id}`
- `POST /api/tasks/{id}/refresh`
- `POST /api/tasks/{id}/run`
- `POST /api/tasks/{id}/miaospeed/run`
- `GET /api/tasks/{id}/miaospeed/results`
- `GET /api/miaospeed/catalog`
- `GET /api/nodes`
- `GET /api/nodes/{id}`
- `GET /api/nodes/{id}/history`
- `GET /api/stats`
- `POST /api/tests/run`
- `GET /metrics`

后续新增能力优先扩展现有响应结构中的 `metrics`、`meta` 和 `score_breakdown`，避免为每个新指标新增一套列表/详情 API。

## 验证命令

```bash
export PATH="/usr/local/go/bin:$PATH"
go test ./...

export PATH="/opt/homebrew/opt/node@22/bin:$PATH"
npm --prefix frontend run build
npm --prefix frontend audit --audit-level=moderate

git diff --check
```

MiaoSpeed opt-in 集成测试：

```bash
PROXY_CHECK_MIAOSPEED_INTEGRATION=1 \
MIAOSPEED_BIN=/path/to/miaospeed \
MIAOSPEED_TOKEN=your_token \
go test ./backend/internal/miaospeed -run TestMiaoSpeedSidecarIntegration -count=1 -v
```
