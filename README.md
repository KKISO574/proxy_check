# Proxy Check 节点质量检测平台

Proxy Check 是一个基于 Clash/Mihomo 的节点质量检测平台。首版包含：

- FastAPI 后端 API
- React 可视化页面
- Clash/Mihomo YAML 静态节点读取
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
- `mihomo.source_config_path`：你的 Clash/Mihomo YAML 配置文件路径
- `mihomo.secret_env`：保存 Mihomo external-controller secret 的环境变量名

示例：

```bash
export MIHOMO_SECRET="your_secret"
```

首版只读取 YAML 里的静态 `proxies`。订阅链接、`proxy-providers`、解锁检测、测速等功能后续再加。

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

首版接口：

- `GET /api/nodes`：节点列表和最新检测结果
- `GET /api/nodes/{id}`：节点详情和最近错误
- `GET /api/nodes/{id}/history?metric=delay|tcping&range=1h|6h|24h|7d|30d`：历史折线数据
- `GET /api/stats`：全局统计
- `POST /api/tests/run`：立即触发一轮检测

## 检测逻辑

- 真延迟：调用 Mihomo External Controller 的 `/proxies/{name}/delay`。
- tcping：为每个节点生成一个本地 `mixed` listener，然后通过这个 listener 对目标地址发起 SOCKS5 TCP CONNECT。
- 默认 tcping 目标：
  - `1.1.1.1:443`
  - `1.1.1.1:80`
  - `8.8.8.8:443`
  - `8.8.8.8:80`

默认每 60 秒检测一轮，历史数据保留 30 天。

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
