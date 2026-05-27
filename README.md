# Proxy Check 节点质量检测平台

Proxy Check 是一个基于 Clash/Mihomo 的节点质量检测平台。首版包含：

- FastAPI 后端 API
- React 可视化页面
- Clash/Mihomo YAML 静态节点读取和 URL 导入
- 多配置监测任务，每个任务可设置检测间隔
- Mihomo 进程托管
- 真延迟检测，不使用简单 ping
- 通过节点链路做 tcping
- SQLite 历史数据
- 节点列表、状态、延迟折线图、tcping 折线图

## 本地启动

```bash
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload
```

启动后访问：

- 页面：`http://127.0.0.1:8000/`
- API：`http://127.0.0.1:8000/api`
- 文档：`http://127.0.0.1:8000/docs`

## 配置

复制配置模板：

```bash
cp configs/config.example.yaml configs/config.yaml
```

需要重点修改：

- `mihomo.bin`：Mihomo 二进制路径，默认是 `./runtime/bin/mihomo`
- `mihomo.source_config_path`：兼容旧模式的本地 Clash/Mihomo YAML 配置文件路径
- `mihomo.imported_config_dir`：通过页面导入的配置文件保存目录
- `mihomo.secret_env`：保存 Mihomo external-controller secret 的环境变量名

示例：

```bash
export MIHOMO_SECRET="your_secret"
```

首版只识别 Clash/Mihomo YAML 里的静态 `proxies`。页面支持导入返回 Clash YAML 的 URL，然后保存为监测任务并提取节点。Base64 节点订阅、`proxy-providers` 展开、解锁检测、测速等功能后续再加。

## 配置 URL 导入和监测任务

页面左侧是监测任务列表，点击“导入配置 URL”可以创建任务：

- `任务名称`：用于区分不同配置源。
- `Clash 配置 URL`：必须是 `http://` 或 `https://`，响应内容必须是 Clash/Mihomo YAML。
- `检测间隔`：该任务自己的检测周期，默认 60 秒。

导入成功后平台会：

- 下载配置并校验 `proxies` 列表。
- 按同一任务内的节点名去重。
- 保存配置到 `mihomo.imported_config_dir`。
- 同步节点列表，但不会立即检测；可以点击“检测任务”手动运行，或等待任务定时检测。

不同任务里的同名节点互不合并，历史数据也按任务隔离。配置 URL 不会在每一轮检测前自动下载，只有创建任务、编辑 URL 或点击刷新配置时才重新下载。

## 下载 Mihomo

如果本机没有 Mihomo，可以下载到项目目录中用于测试。

本机 macOS 测试：

```bash
python3 scripts/download_mihomo.py --os darwin --arch arm64 --version v1.19.24
```

远端 Linux x86_64 测试：

```bash
python3 scripts/download_mihomo.py --os linux --arch amd64 --version v1.19.24
```

也可以不写 `--version`，脚本会调用 GitHub latest API 自动选择最新版本；如果遇到 GitHub API rate limit，就使用带版本号的命令。

下载后的文件默认在：

```text
runtime/bin/mihomo
```

`runtime/` 默认不提交到 Git 仓库，避免把不同平台的大二进制文件推上去。远端机器需要运行同一个脚本下载对应平台版本。

## Docker 部署

Docker 部署会在镜像里构建前端并运行 FastAPI 后端。Mihomo 二进制、SQLite 数据库和运行时文件通过宿主机目录挂载，方便本地和远端分别使用不同平台的 Mihomo。

准备 Docker 容器使用的 Mihomo。注意 Docker 容器里运行的是 Linux 二进制，即使宿主机是 macOS，也不要把 macOS 版 Mihomo 挂进容器。

```bash
# Linux x86_64 远端或 amd64 容器
python3 scripts/download_mihomo.py --os linux --arch amd64 --version v1.19.24

# Linux arm64 容器
python3 scripts/download_mihomo.py --os linux --arch arm64 --version v1.19.24
```

准备 Clash/Mihomo 节点配置。默认 compose 会读取：

```text
configs/clash.yaml
```

也可以用环境变量指定其他路径：

```bash
export CLASH_CONFIG_PATH=/absolute/path/to/clash.yaml
```

设置 Mihomo 控制器 secret：

```bash
export MIHOMO_SECRET="your_secret"
```

启动：

```bash
docker compose up -d --build
```

如果要把外网端口改成 `3456`：

```bash
APP_PORT=3456 docker compose up -d --build
```

访问：

- 页面：`http://127.0.0.1:8000/`
- API：`http://127.0.0.1:8000/api`
- 文档：`http://127.0.0.1:8000/docs`

查看日志：

```bash
docker compose logs -f proxy-check
```

停止：

```bash
docker compose down
```

Docker 使用 [configs/config.docker.yaml](/Users/celia/Github/proxy_check/configs/config.docker.yaml:1)，默认路径如下：

- Mihomo：`/app/runtime/bin/mihomo`
- Clash 配置：`/app/configs/clash.yaml`
- 导入配置：`/app/runtime/configs/task-{id}.yaml`
- SQLite：`/app/data/proxy_check.sqlite3`

## 前端开发

前端在 `frontend/`，要求 Node.js 22.12+ 和 npm 10.9+。如果本机默认 Node/npm 版本较低，建议用 nvm 切到 Node 22：

```bash
cd frontend
nvm install 22
nvm use
node --version
npm --version
```

本机如果同时存在多个 npm，优先使用 Node 22 这一组：

```bash
PATH=/opt/homebrew/opt/node@22/bin:$PATH npm run build
```

```bash
cd frontend
npm install
npm run dev
```

生产构建：

```bash
cd frontend
npm run build
```

构建产物会输出到 `app/static/`，后端会直接托管页面。

## API

主要接口：

- `GET /api/tasks`：监测任务列表
- `POST /api/tasks`：创建任务并导入 Clash 配置 URL
- `PATCH /api/tasks/{id}`：编辑任务名称、URL、启用状态、检测间隔
- `DELETE /api/tasks/{id}`：删除任务及其节点历史
- `POST /api/tasks/{id}/refresh`：重新下载配置并同步节点
- `POST /api/tasks/{id}/run`：立即检测单个任务
- `GET /api/nodes?task_id={id}`：节点列表和最新检测结果，不传 `task_id` 时返回全局节点
- `GET /api/nodes/{id}`：节点详情和最近错误
- `GET /api/nodes/{id}/history?metric=delay|tcping&range=1h|6h|24h|7d|30d`：历史折线数据
- `GET /api/stats?task_id={id}`：统计信息，不传 `task_id` 时返回全局统计
- `POST /api/tests/run`：兼容旧模式，立即触发旧本地配置检测

## 检测逻辑

- 真延迟：调用 Mihomo External Controller 的 `/proxies/{name}/delay`。
- tcping：为每个节点生成一个本地 `mixed` listener，然后通过这个 listener 对目标地址发起 SOCKS5 TCP CONNECT。
- 默认 tcping 目标：
  - `1.1.1.1:443`
  - `1.1.1.1:80`
  - `8.8.8.8:443`
  - `8.8.8.8:80`

任务默认每 60 秒检测一轮，可在页面中按任务修改；历史数据默认保留 30 天。

## 测试

```bash
pip install -r requirements-dev.txt
pytest -q
```

前端类型检查和构建：

```bash
cd frontend
npm install
npm run build
```
