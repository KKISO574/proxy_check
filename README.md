# Proxy Check 节点质量检测平台

基于 Clash/Mihomo 的代理节点质量检测平台。后端 FastAPI + 前端 React，
Mihomo 进程被托管在后端进程内，节点检测通过 Mihomo External Controller
和每节点独立的 SOCKS5 listener 完成。

长期蓝图与路线图见 [AGENT.md](AGENT.md)。本文件覆盖当前已交付能力与本地运行方式。

## 已实现能力

- **多监测任务**：每个任务对应一个 Clash/Mihomo YAML URL，独立的检测周期与节点列表
- **节点级 listener**：每节点分配独立的 `mixed` 端口，避免在 Mihomo 全局 selector 上反复切换
- **探测维度**（默认全开，可在 `probe.dimensions` 关闭）
  - `delay`：调用 `/proxies/{name}/delay`
  - `tcping`：通过节点 listener 做 SOCKS5 TCP CONNECT
  - `tls_handshake`：通过节点 listener 测 TLS 握手耗时
  - `http_rtt`：通过节点 listener 请求 `https://www.gstatic.com/generate_204`
  - `jitter`：基于最近 20 条 `delay` 样本计算 stddev
  - `packet_loss`：连续 20 次 tcping 计算成功率
  - `exit_geo`：通过节点 listener 请求 `https://ipapi.co/json` 获取出口 IP / ASN / 国家 / 地区 / ISP
- **历史持久化**：SQLite，默认保留 30 天
- **节点评分**：API 与页面展示 0-100 分、置信度和分项贡献；评分只读计算，不落库
- **可观测性**：Prometheus 文本指标 `/metrics`，Grafana 示例见 `docs/grafana/proxy-check-v3.json`
- **可视化**：节点列表、状态徽章、评分排序、按 metric 分 tab 的折线图、节点画像卡片、按国家/ASN 过滤
- **部署**：Docker 多阶段构建 + docker-compose

当前版本只识别 Clash/Mihomo YAML 里的静态 `proxies`。
Base64 订阅、`proxy-providers` 展开、流媒体解锁、DNS 泄漏、带宽测速等
功能在路线图中，详见 [AGENT.md](AGENT.md)。

## 快速开始

```bash
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload
```

启动后访问：

- 页面：`http://127.0.0.1:8000/`
- API：`http://127.0.0.1:8000/api`
- OpenAPI：`http://127.0.0.1:8000/docs`

## 配置

```bash
cp configs/config.example.yaml configs/config.yaml
```

需要重点关注的字段：

| 字段 | 说明 |
|---|---|
| `mihomo.bin` | Mihomo 二进制路径，默认 `./runtime/bin/mihomo` |
| `mihomo.imported_config_dir` | 导入的配置 YAML 缓存目录，默认 `./runtime/configs` |
| `mihomo.listener_port_start` / `listener_port_max` | 节点 listener 的端口范围，默认 `20000-65000` |
| `mihomo.secret_env` | Mihomo external-controller secret 所在的环境变量名 |
| `probe.interval_seconds` | 任务级默认检测周期 |
| `probe.timeout_ms` | 单次探测超时 |
| `probe.import_timeout_ms` | URL 导入超时（默认 30 秒，与探测超时分开） |
| `probe.dimensions` | 启用的探测维度 |
| `probe.tcp_targets` | tcping 与 packet_loss 的目标 |

Mihomo controller secret 必须从环境变量读取，不进配置文件：

```bash
export MIHOMO_SECRET="your_secret"
```

## 下载 Mihomo

如果本机或镜像里没有 Mihomo 二进制，用脚本下载：

```bash
# 本地 macOS 测试
python3 scripts/download_mihomo.py --os darwin --arch arm64 --version v1.19.24

# 远端 Linux x86_64 / Docker amd64
python3 scripts/download_mihomo.py --os linux --arch amd64 --version v1.19.24

# Linux arm64
python3 scripts/download_mihomo.py --os linux --arch arm64 --version v1.19.24
```

不带 `--version` 时脚本走 GitHub latest API。下载文件落到 `runtime/bin/mihomo`，
`runtime/` 默认不入 Git，跨平台部署各自下载即可。

## Docker 部署

镜像在构建期编译前端、把 FastAPI 和 Mihomo 打到一起。
SQLite 数据库、Mihomo 二进制和导入的配置都通过宿主机目录挂载，
方便本地与远端使用不同平台的 Mihomo。

注意：容器跑的是 Linux 二进制，宿主机即使是 macOS，也不要把
macOS 版 Mihomo 挂进容器。

```bash
# 默认从 configs/clash.yaml 读取本地配置
export MIHOMO_SECRET="your_secret"
docker compose up -d --build
```

或自定义 host 端口：

```bash
APP_PORT=3456 docker compose up -d --build
```

容器内默认路径见 `configs/config.docker.yaml`：

- Mihomo：`/app/runtime/bin/mihomo`
- 本地 Clash 配置：`/app/configs/clash.yaml`
- 导入的任务配置：`/app/runtime/configs/task-{id}.yaml`
- SQLite：`/app/data/proxy_check.sqlite3`

日志与停止：

```bash
docker compose logs -f proxy-check
docker compose down
```

## 监测任务

页面左侧是任务列表，点"导入配置 URL"创建任务。任务级别的字段：

- `任务名称`：用于区分不同配置源
- `Clash 配置 URL`：必须 `http://` 或 `https://`，响应必须是 Clash/Mihomo YAML
- `检测间隔`：该任务自己的检测周期，最低 10 秒，默认 60 秒

任务行为：

- 创建任务、编辑 URL、手动点"刷新配置"时才重新下载 URL；
  普通检测轮使用缓存的 YAML
- 同任务内按节点名去重；不同任务里的同名节点**互不合并**
- 每个任务独占一段 listener 端口，跨任务不冲突
- 删除任务会删除其所有节点和历史记录

## API

主要接口：

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/tasks` | 任务列表，含节点数、最近运行状态 |
| POST | `/api/tasks` | 创建任务并导入 URL |
| PATCH | `/api/tasks/{id}` | 编辑任务字段；改 URL 会触发刷新 |
| DELETE | `/api/tasks/{id}` | 删除任务及其节点历史 |
| POST | `/api/tasks/{id}/refresh` | 重新下载 URL 并同步节点 |
| POST | `/api/tasks/{id}/run` | 立即检测单个任务 |
| GET | `/api/nodes` | 节点列表，可选 `?task_id=` |
| GET | `/api/nodes/{id}` | 节点详情，含最近错误 |
| GET | `/api/nodes/{id}/history` | 历史折线，参数 `metric` + `range=1h\|6h\|24h\|7d\|30d` |
| GET | `/api/stats` | 全局或单任务统计，可选 `?task_id=` |
| POST | `/api/tests/run` | 兼容旧本地配置的全量检测 |
| GET | `/metrics` | Prometheus 文本指标，可选 `?task_id=` |

节点列表与详情会额外返回：

- `score`：0-100 综合分；无数据时为 `null`
- `score_confidence`：参与评分的权重占比，0-1
- `score_breakdown`：`delay`、`packet_loss`、`jitter`、`transport`、`status` 分项

默认评分权重：delay 35、packet_loss 25、jitter 15、tcp/http/tls transport 15、status 10。

## Prometheus / Grafana

Prometheus 抓取示例：

```yaml
scrape_configs:
  - job_name: proxy-check
    static_configs:
      - targets: ["127.0.0.1:8000"]
```

当前导出：

- `proxy_check_node_score`
- `proxy_check_node_score_confidence`
- `proxy_check_node_availability`
- `proxy_check_node_metric_latency_ms`
- `proxy_check_node_metric_value`

Grafana 可导入 `docs/grafana/proxy-check-v3.json` 作为起点，数据源选择 Prometheus。

## 检测说明

- 默认每 60 秒一轮，由各任务的 `interval_seconds` 控制
- 单轮内并发数受 `probe.concurrency` 限制（默认 100）
- 默认 tcping 目标：`1.1.1.1:443/80`、`8.8.8.8:443/80`
- 历史记录默认保留 30 天，由 `probe.retention_days` 控制
- 每节点 listener 端口在 `[listener_port_start, listener_port_max]`
  内通过 gap-finding 分配；范围内可分配 ~45000 个槽位（同时跨所有任务）
- 日志默认使用 JSON 行格式，方便 Docker / 日志平台采集

## 前端开发

要求 Node.js 22.12+ 与 npm 10.9+。如果默认 Node 较低，建议用 nvm：

```bash
cd frontend
nvm install 22 && nvm use
npm install
npm run dev
```

构建：

```bash
cd frontend
npm run build
```

构建产物落到 `app/static/`，由 FastAPI 直接托管。

## 测试

```bash
pip install -r requirements-dev.txt
pytest -q
```

前端类型检查同 `npm run build`。
