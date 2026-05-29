import React, { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CirclePause,
  Download,
  Gauge,
  Globe2,
  Moon,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  Sun,
  Trash2,
  UnlockKeyhole,
  XCircle
} from "lucide-react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis
} from "recharts";
import {
  createTask,
  deleteTask,
  fetchHistory,
  fetchNode,
  fetchNodes,
  fetchStats,
  fetchTasks,
  MonitorTask,
  NodeDetail,
  NodeItem,
  ProbePoint,
  refreshTask,
  runMiaoSpeedTask,
  runTask,
  Stats,
  updateTask
} from "./api";
import "./styles.css";

type StatusFilter = "all" | "available" | "down" | "unknown";
type RangeFilter = "1h" | "6h" | "24h" | "7d" | "30d";
type SortKey = "name" | "status" | "delay" | "score";
type PageKey = "monitor" | "ip" | "dns";
type ChartMetric = {
  key: string;
  label: string;
  color: string;
};

const NAV_ITEMS: { key: PageKey; path: string; label: string; short: string }[] = [
  { key: "monitor", path: "/", label: "节点监控", short: "监控" },
  { key: "ip", path: "/ip/", label: "IP 画像", short: "IP" },
  { key: "dns", path: "/dns/", label: "DNS 泄露检测", short: "DNS" }
];

const DEFAULT_CHART_METRICS: ChartMetric[] = [
  { key: "delay", label: "真延迟", color: "#16a34a" },
  { key: "tcping", label: "tcping", color: "#2563eb" }
];

const EMPTY_STATS: Stats = {
  total_nodes: 0,
  available_nodes: 0,
  down_nodes: 0,
  unknown_nodes: 0,
  average_delay_ms: null
};

const METRIC_LABELS: Record<string, string> = {
  delay: "真延迟",
  tcping: "tcping",
  tls_handshake: "TLS 握手",
  http_rtt: "HTTP RTT",
  jitter: "抖动",
  packet_loss: "丢包率",
  exit_geo: "出口画像",
  miaospeed_bandwidth: "带宽",
  miaospeed_dns_leak: "DNS 泄漏",
  miaospeed_unlock: "解锁"
};

function isPresent(value: string | null | undefined): value is string {
  return Boolean(value);
}

function formatLatency(value: number | null): string {
  if (value === null || Number.isNaN(value)) return "-";
  if (value >= 1000) return `${(value / 1000).toFixed(2)}s`;
  return `${Math.round(value)}ms`;
}

function formatScore(value: number | null): string {
  if (value === null || Number.isNaN(value)) return "-";
  return `${Math.round(value)}`;
}

function formatThroughput(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return "-";
  if (value >= 1000) return `${(value / 1000).toFixed(2)} Gbps`;
  return `${value.toFixed(value >= 100 ? 0 : 1)} Mbps`;
}

function formatStatusValue(value: string | null | undefined): string {
  return value || "-";
}

function parseMetricData(data: string | null | undefined): Record<string, unknown> {
  if (!data) return {};
  try {
    const parsed = JSON.parse(data);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function numberFromData(data: Record<string, unknown>, key: string): number | null {
  const value = data[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function formatTime(value: string | null): string {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  }).format(new Date(value));
}

function pageFromPath(pathname: string): PageKey {
  if (pathname.startsWith("/ip")) return "ip";
  if (pathname.startsWith("/dns")) return "dns";
  return "monitor";
}

function activeNode(selected: NodeItem | null, detail: NodeDetail | null): NodeItem | NodeDetail | null {
  if (selected && detail?.id === selected.id) return detail;
  return selected;
}

function statusLabel(status: string): string {
  if (status === "available") return "可用";
  if (status === "down") return "异常";
  if (status === "removed") return "已移除";
  return "未知";
}

function StatusBadge({ status }: { status: string }) {
  const Icon = status === "available" ? CheckCircle2 : status === "down" ? XCircle : AlertTriangle;
  return (
    <span className={`status status-${status}`}>
      <Icon size={14} />
      {statusLabel(status)}
    </span>
  );
}

function MetricCard({
  icon,
  label,
  value,
  tone
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  tone?: string;
}) {
  return (
    <section className={`metric-card ${tone ?? ""}`}>
      <div className="metric-icon">{icon}</div>
      <div>
        <p>{label}</p>
        <strong>{value}</strong>
      </div>
    </section>
  );
}

function NetCoffeeShell({
  page,
  onNavigate,
  children
}: {
  page: PageKey;
  onNavigate: (page: PageKey) => void;
  children: React.ReactNode;
}) {
  const [theme, setTheme] = useState<"light" | "dark">(() => {
    const stored = window.localStorage.getItem("theme");
    return stored === "light" || stored === "dark" ? stored : "dark";
  });

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    window.localStorage.setItem("theme", theme);
  }, [theme]);

  return (
    <div className="nc-page">
      <div className="nc-container">
        <nav className="nc-nav">
          {NAV_ITEMS.map((item) => (
            <button
              key={item.key}
              className={page === item.key ? "active" : ""}
              type="button"
              onClick={() => onNavigate(item.key)}
            >
              <span className="full">{item.label}</span>
              <span className="short">{item.short}</span>
            </button>
          ))}
          <button
            className="theme-toggle"
            type="button"
            title={theme === "dark" ? "切换亮色模式" : "切换暗黑模式"}
            aria-label={theme === "dark" ? "切换亮色模式" : "切换暗黑模式"}
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          >
            {theme === "dark" ? <Moon size={14} /> : <Sun size={14} />}
          </button>
        </nav>
        {children}
        <footer className="nc-footer">© 2026 Proxy Check · 节点质量检测 · IP 画像 · DNS 泄露检测</footer>
      </div>
    </div>
  );
}

function TaskSelector({
  tasks,
  selectedTask,
  selectedTaskId,
  busyTaskId,
  onSelect,
  onCreate,
  onEdit,
  onRefresh,
  onRun,
  onToggle,
  onDelete
}: {
  tasks: MonitorTask[];
  selectedTask: MonitorTask | null;
  selectedTaskId: number | null;
  busyTaskId: number | null;
  onSelect: (id: number) => void;
  onCreate: () => void;
  onEdit: () => void;
  onRefresh: (id: number) => void;
  onRun: () => void;
  onToggle: () => void;
  onDelete: () => void;
}) {
  return (
    <section className="nc-taskbar">
      <div>
        <span className="nc-muted-label">Monitor Task</span>
        <h2>{selectedTask?.name ?? "未选择任务"}</h2>
        <p>{selectedTask?.source_url ?? "导入 Clash/Mihomo YAML URL 后开始同步节点。"}</p>
      </div>
      <div className="nc-task-actions">
        <select value={selectedTaskId ?? ""} onChange={(event) => onSelect(Number(event.target.value))} disabled={tasks.length === 0}>
          {tasks.length === 0 ? (
            <option value="">暂无任务</option>
          ) : (
            tasks.map((task) => (
              <option key={task.id} value={task.id}>
                {task.name} · {task.node_count}
              </option>
            ))
          )}
        </select>
        <button type="button" onClick={onCreate}>
          <Plus size={15} />
          导入
        </button>
        <button type="button" onClick={onEdit} disabled={!selectedTask}>
          <Pencil size={15} />
          编辑
        </button>
        <button type="button" onClick={() => selectedTask && onRefresh(selectedTask.id)} disabled={!selectedTask || busyTaskId === selectedTask?.id}>
          <RefreshCw size={15} className={busyTaskId === selectedTask?.id ? "spin" : ""} />
          刷新
        </button>
        <button type="button" onClick={onToggle} disabled={!selectedTask}>
          <CirclePause size={15} />
          {selectedTask?.enabled ? "暂停" : "启用"}
        </button>
        <button className="danger" type="button" onClick={onDelete} disabled={!selectedTask || busyTaskId === selectedTask?.id}>
          <Trash2 size={15} />
          删除
        </button>
        <button className="strong" type="button" onClick={onRun} disabled={!selectedTask || busyTaskId === selectedTask?.id}>
          <RefreshCw size={15} className={busyTaskId === selectedTask?.id ? "spin" : ""} />
          检测
        </button>
      </div>
    </section>
  );
}

function InfoCard({
  title,
  children,
  action
}: {
  title: string;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <section className="nc-card">
      <div className="nc-card-title">
        <h3>{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

function KeyValueRows({ rows }: { rows: [string, React.ReactNode][] }) {
  return (
    <div className="kv-list">
      {rows.map(([label, value]) => (
        <div className="kv-row" key={label}>
          <span>{label}</span>
          <strong>{value ?? "-"}</strong>
        </div>
      ))}
    </div>
  );
}

function pillClass(value: string | null | undefined): string {
  if (!value) return "pill muted-pill";
  if (isPositiveStatus(value)) return "pill ok-pill";
  const lower = value.toLowerCase();
  if (lower.includes("risk") || lower.includes("leak") || lower.includes("泄露") || lower.includes("失败")) return "pill danger-pill";
  return "pill warn-pill";
}

function scoreClass(score: number | null | undefined): string {
  if (score === null || score === undefined) return "score-mid";
  if (score >= 80) return "score-high";
  if (score >= 50) return "score-mid";
  return "score-low";
}

function MonitorPage({
  selectedTask,
  stats,
  selected,
  detail,
  filtered,
  selectedId,
  loading,
  histories,
  range,
  search,
  statusFilter,
  countryFilter,
  asnFilter,
  sortKey,
  countries,
  asns,
  onSelectNode,
  onRangeChange,
  setSearch,
  setStatusFilter,
  setCountryFilter,
  setAsnFilter,
  setSortKey
}: {
  selectedTask: MonitorTask | null;
  stats: Stats | null;
  selected: NodeItem | null;
  detail: NodeDetail | null;
  filtered: NodeItem[];
  selectedId: number | null;
  loading: boolean;
  histories: Record<string, ProbePoint[]>;
  range: RangeFilter;
  search: string;
  statusFilter: StatusFilter;
  countryFilter: string;
  asnFilter: string;
  sortKey: SortKey;
  countries: string[];
  asns: string[];
  onSelectNode: (node: NodeItem) => void;
  onRangeChange: (range: RangeFilter) => void;
  setSearch: (value: string) => void;
  setStatusFilter: (value: StatusFilter) => void;
  setCountryFilter: (value: string) => void;
  setAsnFilter: (value: string) => void;
  setSortKey: (value: SortKey) => void;
}) {
  return (
    <>
      <header className="nc-header">
        <h1>节点质量监控</h1>
        <p>导入 Clash/Mihomo 配置，按节点持续追踪真延迟、tcping、出口画像、评分和高级探测结果。</p>
      </header>

      <div className="nc-metrics">
        <MetricCard icon={<Server size={20} />} label="节点总数" value={`${stats?.total_nodes ?? 0}`} />
        <MetricCard icon={<CheckCircle2 size={20} />} label="可用节点" value={`${stats?.available_nodes ?? 0}`} tone="ok" />
        <MetricCard icon={<XCircle size={20} />} label="异常节点" value={`${stats?.down_nodes ?? 0}`} tone="danger" />
        <MetricCard icon={<Gauge size={20} />} label="平均延迟" value={formatLatency(stats?.average_delay_ms ?? null)} />
      </div>

      <SignalBoard selected={selected} detail={detail} stats={stats} />

      <section className="nc-grid-main">
        <section className="list-panel nc-card no-padding">
          <div className="toolbar">
            <div className="search-box">
              <Search size={16} />
              <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索节点、入口、出口 IP、ASN" />
            </div>
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as StatusFilter)}>
              <option value="all">全部状态</option>
              <option value="available">可用</option>
              <option value="down">异常</option>
              <option value="unknown">未知</option>
            </select>
            <select value={countryFilter} onChange={(event) => setCountryFilter(event.target.value)}>
              <option value="all">全部国家</option>
              {countries.map((country) => (
                <option key={country} value={country}>
                  {country}
                </option>
              ))}
            </select>
            <select value={asnFilter} onChange={(event) => setAsnFilter(event.target.value)}>
              <option value="all">全部 ASN</option>
              {asns.map((asn) => (
                <option key={asn} value={asn}>
                  {asn}
                </option>
              ))}
            </select>
            <select value={sortKey} onChange={(event) => setSortKey(event.target.value as SortKey)}>
              <option value="delay">按延迟</option>
              <option value="score">按评分</option>
              <option value="status">按状态</option>
              <option value="name">按名称</option>
            </select>
          </div>
          {loading ? (
            <div className="loading">
              <Activity size={20} className="spin" />
              加载中
            </div>
          ) : (
            <NodeTable nodes={filtered} selectedId={selectedId} onSelect={onSelectNode} hasTask={Boolean(selectedTask)} />
          )}
        </section>

        <DetailPane selected={selected} detail={detail} histories={histories} range={range} onRangeChange={onRangeChange} />
      </section>
    </>
  );
}

function IpProfilePage({
  nodes,
  selected,
  detail,
  search,
  setSearch,
  onSelectNode
}: {
  nodes: NodeItem[];
  selected: NodeItem | null;
  detail: NodeDetail | null;
  search: string;
  setSearch: (value: string) => void;
  onSelectNode: (node: NodeItem) => void;
}) {
  const node = activeNode(selected, detail);
  const meta = node?.meta;
  const metrics = node?.metrics ?? {};
  const searchTerm = search.trim().toLowerCase();
  const candidates = nodes.filter((item) => {
    if (!searchTerm) return true;
    return (
      item.name.toLowerCase().includes(searchTerm) ||
      (item.meta?.exit_ip ?? "").toLowerCase().includes(searchTerm) ||
      (item.meta?.asn ?? "").toLowerCase().includes(searchTerm) ||
      (item.meta?.country ?? "").toLowerCase().includes(searchTerm)
    );
  });
  const latencyTargets: [string, React.ReactNode][] = [
    ["真延迟", formatLatency(metrics.delay?.latency_ms ?? null)],
    ["HTTP RTT", formatLatency(metrics.http_rtt?.latency_ms ?? metrics.http_rtt?.value ?? null)],
    ["TLS 握手", formatLatency(metrics.tls_handshake?.latency_ms ?? metrics.tls_handshake?.value ?? null)],
    ["丢包率", metrics.packet_loss?.value === undefined || metrics.packet_loss?.value === null ? "-" : `${metrics.packet_loss.value}%`]
  ];

  return (
    <>
      <header className="nc-header">
        <h1>IP 评分查询</h1>
        <p>把每个代理节点当成独立出口 IP 查看：评分、地理、ASN、运营商、风险和网络质量集中展示。</p>
      </header>

      <section className="ip-head clone-card">
        <div className="ip-main">
          <strong>{meta?.exit_ip ?? node?.server ?? "未选择节点"}</strong>
          <span>
            <Globe2 size={15} />
            {meta?.country ?? "Unknown"} {meta?.region ? `· ${meta.region}` : ""}
          </span>
        </div>
        <div className={`score-gauge ${scoreClass(node?.score)}`}>
          <span>IP 评分</span>
          <strong>{formatScore(node?.score ?? null)}</strong>
        </div>
        <div className="ip-search">
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索节点 / IP / ASN" />
        </div>
      </section>

      <div className="ip-grid">
        <InfoCard title="使用场景 / 类型">
          <KeyValueRows
            rows={[
              ["节点名称", node?.name ?? "-"],
              ["代理类型", node?.type ?? "unknown"],
              ["节点状态", <StatusBadge status={node?.status ?? "unknown"} />],
              ["出口 DNS", <span className={pillClass(meta?.dns_leak)}>{formatStatusValue(meta?.dns_leak)}</span>]
            ]}
          />
        </InfoCard>
        <InfoCard title="ASN / 运营商">
          <KeyValueRows
            rows={[
              ["ASN", meta?.asn ?? "-"],
              ["ISP", meta?.isp ?? "-"],
              ["国家 / 地区", `${meta?.country ?? "-"}${meta?.region ? ` / ${meta.region}` : ""}`],
              ["入口", `${node?.server ?? "-"}${node?.port ? `:${node.port}` : ""}`]
            ]}
          />
        </InfoCard>
        <InfoCard title="技术指标">
          <KeyValueRows rows={latencyTargets} />
        </InfoCard>
        <InfoCard title="IP 情报（威胁指标）">
          <KeyValueRows
            rows={[
              ["Netflix", <span className={pillClass(meta?.netflix_unlock)}>{formatStatusValue(meta?.netflix_unlock)}</span>],
              ["Disney+", <span className={pillClass(meta?.disney_unlock)}>{formatStatusValue(meta?.disney_unlock)}</span>],
              ["OpenAI", <span className={pillClass(meta?.openai_unlock)}>{formatStatusValue(meta?.openai_unlock)}</span>],
              ["YouTube", <span className={pillClass(meta?.youtube_unlock)}>{formatStatusValue(meta?.youtube_unlock)}</span>]
            ]}
          />
        </InfoCard>
      </div>

      <InfoCard title="全球主要地区延迟测试">
        <div className="ping-strip">
          {latencyTargets.map(([label, value]) => (
            <div key={label}>
              <span>{label}</span>
              <strong>{value}</strong>
            </div>
          ))}
        </div>
      </InfoCard>

      <InfoCard title="节点 IP 列表">
        {candidates.length === 0 ? (
          <EmptyState hasTask={nodes.length > 0} />
        ) : (
          <div className="ip-node-grid">
            {candidates.map((item) => (
              <button className={item.id === node?.id ? "active" : ""} key={item.id} type="button" onClick={() => onSelectNode(item)}>
                <strong>{item.meta?.exit_ip ?? item.server ?? "-"}</strong>
                <span>{item.name}</span>
                <em>{item.meta?.asn ?? "-"} · {item.meta?.country ?? "-"}</em>
              </button>
            ))}
          </div>
        )}
      </InfoCard>
    </>
  );
}

function DnsLeakPage({
  nodes,
  selectedTask,
  selected,
  detail,
  busyTaskId,
  onRunQuick,
  onRunDeep,
  onSelectNode
}: {
  nodes: NodeItem[];
  selectedTask: MonitorTask | null;
  selected: NodeItem | null;
  detail: NodeDetail | null;
  busyTaskId: number | null;
  onRunQuick: () => void;
  onRunDeep: () => void;
  onSelectNode: (node: NodeItem) => void;
}) {
  const node = activeNode(selected, detail);
  const dnsLeak = node?.meta?.dns_leak;
  const leaked = Boolean(dnsLeak && /leak|泄露|danger|risk|failed|失败/i.test(dnsLeak));
  const clean = Boolean(dnsLeak && !leaked);
  const verdictClass = leaked ? "verdict-danger" : clean ? "verdict-safe" : "verdict-warn";
  const verdictText = leaked ? "检测到 DNS 泄露风险" : clean ? "DNS 当前看起来干净" : "等待 DNS 检测结果";

  return (
    <>
      <header className="nc-header">
        <h1>DNS 泄露检测</h1>
        <p>单独查看节点 DNS 泄露状态，手动触发快速任务或 MiaoSpeed 深度脚本测试。</p>
      </header>

      <div className="dns-feature-grid">
        {[
          ["🔒", "节点级 DNS", "每个代理出口独立记录"],
          ["⚡", "快速测试", "复用低流量任务检测"],
          ["🎯", "深度脚本", "MiaoSpeed TEST_SCRIPT"],
          ["🛡", "结果留存", "写入节点画像和历史"]
        ].map(([icon, title, desc]) => (
          <section className="dns-feature" key={title}>
            <strong>{icon}</strong>
            <span>{title}</span>
            <small>{desc}</small>
          </section>
        ))}
      </div>

      <div className="dns-actions">
        <button type="button" onClick={onRunQuick} disabled={!selectedTask || busyTaskId === selectedTask?.id}>
          <RefreshCw size={16} className={busyTaskId === selectedTask?.id ? "spin" : ""} />
          快速测试
        </button>
        <button type="button" onClick={onRunDeep} disabled={!selectedTask || busyTaskId === selectedTask?.id}>
          <ShieldCheck size={16} className={busyTaskId === selectedTask?.id ? "spin" : ""} />
          深度测试
        </button>
      </div>

      <div className={`dns-verdict ${verdictClass}`}>
        <strong>{verdictText}</strong>
        <span>{node?.name ?? "选择节点后查看 DNS 检测结果"}</span>
      </div>

      <InfoCard title="DNS 检测结果">
        {nodes.length === 0 ? (
          <EmptyState hasTask={Boolean(selectedTask)} />
        ) : (
          <div className="dns-table-wrap">
            <table className="dns-table">
              <thead>
                <tr>
                  <th>节点</th>
                  <th>DNS 状态</th>
                  <th>出口 IP</th>
                  <th>国家 / ASN</th>
                  <th>最近检测</th>
                </tr>
              </thead>
              <tbody>
                {nodes.map((item) => (
                  <tr key={item.id} className={item.id === node?.id ? "selected-row" : ""} onClick={() => onSelectNode(item)}>
                    <td>
                      <div className="node-name">{item.name}</div>
                      <span className="muted">{item.type ?? "unknown"}</span>
                    </td>
                    <td>
                      <span className={pillClass(item.meta?.dns_leak)}>{formatStatusValue(item.meta?.dns_leak)}</span>
                    </td>
                    <td className="mono">{item.meta?.exit_ip ?? "-"}</td>
                    <td>
                      <div>{item.meta?.country ?? "-"}</div>
                      <span className="muted">{item.meta?.asn ?? "-"}</span>
                    </td>
                    <td>{formatTime(item.last_checked_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </InfoCard>

      <section className="article-grid">
        {[
          ["DNS 泄露是什么？", "代理流量走了节点，但 DNS 查询仍从本地网络发出时，真实网络位置可能被暴露。"],
          ["发现泄露后怎么修？", "优先启用代理侧 DNS、关闭系统直连 DNS，并用深度测试复查 resolver 出口。"],
          ["为什么代理后仍会泄露？", "浏览器、系统、透明代理和分流规则都可能让 DNS 查询绕过代理通道。"],
          ["DoH / DoT 有什么区别？", "它们能加密 DNS 查询，但是否经过代理出口仍取决于本地路由和代理规则。"]
        ].map(([title, body]) => (
          <details className="article-fold" key={title}>
            <summary>{title}</summary>
            <p>{body}</p>
          </details>
        ))}
      </section>
    </>
  );
}

function isPositiveStatus(value: string | null | undefined): boolean {
  if (!value) return false;
  const normalized = value.toLowerCase();
  return ["full", "available", "unlock", "unlocked", "allow", "allowed", "support", "yes", "true", "ok"].some((token) =>
    normalized.includes(token)
  );
}

function SignalBoard({
  selected,
  detail,
  stats
}: {
  selected: NodeItem | null;
  detail: NodeDetail | null;
  stats: Stats | null;
}) {
  const activeDetail = selected && detail?.id === selected.id ? detail : null;
  const meta = activeDetail?.meta ?? selected?.meta;
  const metrics = activeDetail?.metrics ?? selected?.metrics;
  const bandwidth = metrics?.miaospeed_bandwidth;
  const bandwidthData = parseMetricData(bandwidth?.data);
  const averageMbps = bandwidth?.value ?? numberFromData(bandwidthData, "average_mbps");
  const unlockItems = [
    ["Netflix", meta?.netflix_unlock],
    ["Disney+", meta?.disney_unlock],
    ["OpenAI", meta?.openai_unlock],
    ["YouTube", meta?.youtube_unlock]
  ];
  const unlockedCount = unlockItems.filter(([, value]) => isPositiveStatus(value)).length;
  const score = activeDetail?.score ?? selected?.score ?? null;
  const confidence = Math.round(((activeDetail?.score_confidence ?? selected?.score_confidence) || 0) * 100);

  return (
    <section className="signal-board">
      <div className="signal-primary">
        <span className="signal-kicker">{selected ? "当前节点画像" : "全局监测概览"}</span>
        <h3>{selected?.name ?? "选择节点查看出口画像"}</h3>
        <p>
          {selected
            ? `${selected.type ?? "unknown"} · listener ${selected.listener_port ?? "-"}`
            : `${stats?.total_nodes ?? 0} 个节点 · 平均延迟 ${formatLatency(stats?.average_delay_ms ?? null)}`}
        </p>
      </div>

      <div className="signal-score">
        <span>评分</span>
        <strong>{formatScore(score)}</strong>
        <small>置信度 {confidence}%</small>
      </div>

      <div className="signal-facts">
        <div>
          <span>出口 IP</span>
          <strong>{meta?.exit_ip ?? "-"}</strong>
        </div>
        <div>
          <span>ASN / GEO</span>
          <strong>
            {meta?.asn ?? "-"} · {meta?.country ?? "-"}
          </strong>
        </div>
        <div>
          <span>ISP</span>
          <strong>{meta?.isp ?? "-"}</strong>
        </div>
      </div>

      <div className="signal-metrics">
        <div>
          <span>真延迟</span>
          <strong>{formatLatency(metrics?.delay?.latency_ms ?? null)}</strong>
        </div>
        <div>
          <span>tcping</span>
          <strong>{formatLatency(metrics?.tcping?.latency_ms ?? null)}</strong>
        </div>
        <div>
          <span>带宽</span>
          <strong>{formatThroughput(averageMbps)}</strong>
        </div>
        <div>
          <span>DNS</span>
          <strong>{formatStatusValue(meta?.dns_leak)}</strong>
        </div>
      </div>

      <div className="signal-unlocks">
        <span>解锁 {unlockedCount}/4</span>
        <div>
          {unlockItems.map(([label, value]) => (
            <em className={isPositiveStatus(value) ? "ok" : ""} key={label}>
              {label}
            </em>
          ))}
        </div>
      </div>
    </section>
  );
}

function TaskForm({
  task,
  onSubmit,
  onCancel,
  saving
}: {
  task: MonitorTask | null;
  onSubmit: (values: { name: string; source_url: string; interval_seconds: number; advanced_probes_enabled: boolean }) => Promise<void>;
  onCancel: () => void;
  saving: boolean;
}) {
  const [name, setName] = useState(task?.name ?? "");
  const [sourceUrl, setSourceUrl] = useState(task?.source_url ?? "");
  const [interval, setInterval] = useState(`${task?.interval_seconds ?? 60}`);
  const [advancedProbesEnabled, setAdvancedProbesEnabled] = useState(task?.advanced_probes_enabled ?? false);

  useEffect(() => {
    setName(task?.name ?? "");
    setSourceUrl(task?.source_url ?? "");
    setInterval(`${task?.interval_seconds ?? 60}`);
    setAdvancedProbesEnabled(task?.advanced_probes_enabled ?? false);
  }, [task]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    await onSubmit({
      name: name.trim(),
      source_url: sourceUrl.trim(),
      interval_seconds: Math.max(10, Number(interval) || 60),
      advanced_probes_enabled: advancedProbesEnabled
    });
  }

  return (
    <form className="task-form" onSubmit={handleSubmit}>
      <div className="form-grid">
        <label>
          <span>任务名称</span>
          <input value={name} onChange={(event) => setName(event.target.value)} required />
        </label>
        <label className="url-field">
          <span>Clash 配置 URL</span>
          <input
            value={sourceUrl}
            onChange={(event) => setSourceUrl(event.target.value)}
            placeholder="https://example.com/clash.yaml"
            required
          />
        </label>
        <label>
          <span>检测间隔</span>
          <input
            min={10}
            step={10}
            type="number"
            value={interval}
            onChange={(event) => setInterval(event.target.value)}
          />
        </label>
        <label className="toggle-field">
          <input
            type="checkbox"
            checked={advancedProbesEnabled}
            onChange={(event) => setAdvancedProbesEnabled(event.target.checked)}
          />
          <span>高级探测</span>
        </label>
      </div>
      <div className="form-actions">
        <button className="ghost-button" type="button" onClick={onCancel}>
          取消
        </button>
        <button className="primary-button" type="submit" disabled={saving}>
          <RefreshCw size={16} className={saving ? "spin" : ""} />
          {task ? "保存任务" : "导入任务"}
        </button>
      </div>
    </form>
  );
}

function EmptyState({ hasTask }: { hasTask: boolean }) {
  return (
    <div className="empty-state">
      <Server size={28} />
      <strong>{hasTask ? "暂无节点" : "暂无监测任务"}</strong>
      <span>{hasTask ? "刷新配置后会同步 Clash YAML 中的节点。" : "导入 Clash/Mihomo YAML URL 后开始监测。"}</span>
    </div>
  );
}

function NodeTable({
  nodes,
  selectedId,
  onSelect,
  hasTask
}: {
  nodes: NodeItem[];
  selectedId: number | null;
  onSelect: (node: NodeItem) => void;
  hasTask: boolean;
}) {
  if (nodes.length === 0) return <EmptyState hasTask={hasTask} />;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>节点</th>
            <th>状态</th>
            <th>评分</th>
            <th>地区</th>
            <th>真延迟</th>
            <th>tcping</th>
            <th>入口</th>
            <th>最近检测</th>
          </tr>
        </thead>
        <tbody>
          {nodes.map((node) => (
            <tr
              key={node.id}
              className={selectedId === node.id ? "selected-row" : ""}
              onClick={() => onSelect(node)}
            >
              <td>
                <div className="node-name">{node.name}</div>
                <span className="muted">{node.type ?? "unknown"}</span>
              </td>
              <td>
                <StatusBadge status={node.status} />
              </td>
              <td>
                <strong>{formatScore(node.score)}</strong>
                <span className="muted">{Math.round(node.score_confidence * 100)}%</span>
              </td>
              <td>
                <div>{node.meta?.country ?? "-"}</div>
                <span className="muted">{node.meta?.asn ?? "-"}</span>
              </td>
              <td>{formatLatency(node.metrics.delay?.latency_ms ?? null)}</td>
              <td>
                <div>{formatLatency(node.metrics.tcping?.latency_ms ?? null)}</div>
                <span className="muted">{node.metrics.tcping?.target ?? "-"}</span>
              </td>
              <td>
                <span className="mono">
                  {node.server ?? "-"}
                  {node.port ? `:${node.port}` : ""}
                </span>
              </td>
              <td>{formatTime(node.last_checked_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ChartPanel({ title, points, color }: { title: string; points: ProbePoint[]; color: string }) {
  const data = points.map((point) => ({
    time: formatTime(point.created_at),
    value: point.success ? point.latency_ms ?? point.value : null,
    target: point.target
  }));

  return (
    <section className="chart-panel">
      <div className="panel-title">
        <h3>{title}</h3>
        <span>{points.length} samples</span>
      </div>
      <div className="chart-box">
        {data.length === 0 ? (
          <div className="chart-empty">暂无历史数据</div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={data}>
              <CartesianGrid strokeDasharray="3 3" stroke="#dde6ef" />
              <XAxis dataKey="time" tick={{ fontSize: 11 }} minTickGap={24} />
              <YAxis tick={{ fontSize: 11 }} width={52} />
              <Tooltip
                contentStyle={{
                  border: "1px solid #d8e2ec",
                  borderRadius: 8,
                  boxShadow: "0 12px 30px rgba(15, 23, 42, 0.12)"
                }}
              />
              <Line
                type="monotone"
                dataKey="value"
                stroke={color}
                strokeWidth={2}
                dot={false}
                connectNulls={false}
                name="value"
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </section>
  );
}

function ScorePanel({ detail }: { detail: NodeDetail | null }) {
  const entries = Object.entries(detail?.score_breakdown ?? {});
  return (
    <section className="score-panel">
      <div className="panel-title">
        <h3>节点评分</h3>
        <span>{detail?.score === null || detail?.score === undefined ? "-" : `${Math.round(detail.score)} / 100`}</span>
      </div>
      <div className="score-bar">
        <span style={{ width: `${Math.max(0, Math.min(100, detail?.score ?? 0))}%` }} />
      </div>
      <div className="score-confidence">数据置信度 {Math.round((detail?.score_confidence ?? 0) * 100)}%</div>
      <div className="score-breakdown">
        {entries.length === 0 ? (
          <div className="muted">暂无评分数据</div>
        ) : (
          entries.map(([name, item]) => (
            <div className="score-row" key={name}>
              <span>{name}</span>
              <strong>{Math.round(item.score)}</strong>
              <small>{item.weight}%</small>
            </div>
          ))
        )}
      </div>
    </section>
  );
}

function MetaPanel({ detail }: { detail: NodeDetail | null }) {
  const meta = detail?.meta;
  const items = [
    ["出口 IP", meta?.exit_ip],
    ["ASN", meta?.asn],
    ["国家", meta?.country],
    ["地区", meta?.region],
    ["ISP", meta?.isp]
  ];

  return (
    <section className="meta-panel">
      <div className="panel-title">
        <h3>节点画像</h3>
        <span>{meta?.exit_ip ? "已识别" : "待检测"}</span>
      </div>
      <div className="meta-grid">
        {items.map(([label, value]) => (
          <div className="meta-item" key={label}>
            <span>{label}</span>
            <strong>{value || "-"}</strong>
          </div>
        ))}
      </div>
    </section>
  );
}

function AdvancedProbePanel({ detail }: { detail: NodeDetail | null }) {
  const meta = detail?.meta;
  const bandwidth = detail?.metrics.miaospeed_bandwidth;
  const bandwidthData = parseMetricData(bandwidth?.data);
  const averageMbps = bandwidth?.value ?? numberFromData(bandwidthData, "average_mbps");
  const maxMbps = numberFromData(bandwidthData, "max_mbps");
  const unlockItems = [
    ["Netflix", meta?.netflix_unlock],
    ["Disney+", meta?.disney_unlock],
    ["OpenAI", meta?.openai_unlock],
    ["YouTube", meta?.youtube_unlock]
  ];

  return (
    <section className="advanced-panel">
      <div className="panel-title">
        <h3>高级探测</h3>
        <span>{detail?.metrics.miaospeed_bandwidth || meta?.dns_leak ? "已有结果" : "待检测"}</span>
      </div>
      <div className="advanced-summary">
        <div className="advanced-card">
          <div className="advanced-icon">
            <ShieldCheck size={16} />
          </div>
          <span>DNS 泄漏</span>
          <strong>{formatStatusValue(meta?.dns_leak)}</strong>
        </div>
        <div className="advanced-card">
          <div className="advanced-icon">
            <Download size={16} />
          </div>
          <span>平均带宽</span>
          <strong>{formatThroughput(averageMbps)}</strong>
          <small>峰值 {formatThroughput(maxMbps)}</small>
        </div>
      </div>
      <div className="unlock-grid">
        {unlockItems.map(([label, value]) => (
          <div className="unlock-item" key={label}>
            <UnlockKeyhole size={14} />
            <span>{label}</span>
            <strong>{formatStatusValue(value)}</strong>
          </div>
        ))}
      </div>
    </section>
  );
}

function DetailPane({
  selected,
  detail,
  histories,
  range,
  onRangeChange
}: {
  selected: NodeItem | null;
  detail: NodeDetail | null;
  histories: Record<string, ProbePoint[]>;
  range: RangeFilter;
  onRangeChange: (range: RangeFilter) => void;
}) {
  const chartMetrics = useMemo(() => {
    const keys = new Set(DEFAULT_CHART_METRICS.map((metric) => metric.key));
    for (const key of Object.keys(detail?.metrics ?? {})) {
      keys.add(key);
    }
    return Array.from(keys).map((key, index) => {
      const predefined = DEFAULT_CHART_METRICS.find((metric) => metric.key === key);
      if (predefined) return predefined;
      return {
        key,
        label: METRIC_LABELS[key] ?? key,
        color: ["#7c3aed", "#dc2626", "#0891b2", "#ca8a04"][index % 4]
      };
    });
  }, [detail?.metrics]);

  return (
    <aside className="detail-pane">
      {!selected ? (
        <div className="detail-placeholder">选择节点查看趋势和错误</div>
      ) : (
        <>
          <div className="detail-header">
            <div>
              <h2>{selected.name}</h2>
              <p>
                {selected.type ?? "unknown"} · listener {selected.listener_port ?? "-"}
              </p>
            </div>
            <StatusBadge status={selected.status} />
          </div>

          <div className="range-tabs">
            {(["1h", "6h", "24h", "7d", "30d"] as RangeFilter[]).map((item) => (
              <button
                key={item}
                className={range === item ? "active" : ""}
                onClick={() => onRangeChange(item)}
              >
                {item}
              </button>
            ))}
          </div>

          <ScorePanel detail={detail} />
          <MetaPanel detail={detail} />
          <AdvancedProbePanel detail={detail} />

          {chartMetrics.map((metric) => (
            <ChartPanel
              key={metric.key}
              title={metric.label}
              points={histories[metric.key] ?? []}
              color={metric.color}
            />
          ))}

          <section className="error-panel">
            <div className="panel-title">
              <h3>最近错误</h3>
              <span>{detail?.recent_errors.length ?? 0}</span>
            </div>
            <div className="error-list">
              {detail?.recent_errors.length ? (
                detail.recent_errors.map((error, index) => (
                  <div className="error-row" key={`${error.created_at}-${index}`}>
                    <span>{formatTime(error.created_at)}</span>
                    <strong>{error.metric}</strong>
                    <p>{error.error ?? "unknown error"}</p>
                  </div>
                ))
              ) : (
                <div className="muted">暂无错误记录</div>
              )}
            </div>
          </section>
        </>
      )}
    </aside>
  );
}

function App() {
  const [tasks, setTasks] = useState<MonitorTask[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState<number | null>(null);
  const [nodes, setNodes] = useState<NodeItem[]>([]);
  const [stats, setStats] = useState<Stats | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [detail, setDetail] = useState<NodeDetail | null>(null);
  const [histories, setHistories] = useState<Record<string, ProbePoint[]>>({});
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [countryFilter, setCountryFilter] = useState("all");
  const [asnFilter, setAsnFilter] = useState("all");
  const [sortKey, setSortKey] = useState<SortKey>("delay");
  const [search, setSearch] = useState("");
  const [range, setRange] = useState<RangeFilter>("24h");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [editingTask, setEditingTask] = useState<MonitorTask | null>(null);
  const [savingTask, setSavingTask] = useState(false);
  const [busyTaskId, setBusyTaskId] = useState<number | null>(null);
  const [page, setPage] = useState<PageKey>(() => pageFromPath(window.location.pathname));
  const selectedTaskIdRef = useRef<number | null>(selectedTaskId);
  const selectedIdRef = useRef<number | null>(selectedId);

  const selectedTask = tasks.find((task) => task.id === selectedTaskId) ?? null;

  function navigate(pageKey: PageKey) {
    const target = NAV_ITEMS.find((item) => item.key === pageKey);
    if (!target) return;
    window.history.pushState({}, "", target.path);
    setPage(pageKey);
  }

  function selectTaskId(taskId: number | null) {
    selectedTaskIdRef.current = taskId;
    setSelectedTaskId(taskId);
  }

  function selectNodeId(nodeId: number | null) {
    selectedIdRef.current = nodeId;
    setSelectedId(nodeId);
  }

  function clearOverview() {
    setNodes([]);
    setStats(EMPTY_STATS);
    selectNodeId(null);
    setDetail(null);
    setHistories({});
  }

  async function loadTasks(nextSelectedId = selectedTaskIdRef.current) {
    const nextTasks = await fetchTasks();
    setTasks(nextTasks);
    if (nextTasks.length === 0) {
      selectTaskId(null);
      return null;
    }
    const stillExists = nextTasks.some((task) => task.id === nextSelectedId);
    const nextId = stillExists ? nextSelectedId : nextTasks[0].id;
    selectTaskId(nextId);
    return nextId;
  }

  async function loadOverview(taskId = selectedTaskIdRef.current) {
    try {
      const [nextNodes, nextStats] = await Promise.all([fetchNodes(taskId), fetchStats(taskId)]);
      setNodes(nextNodes);
      setStats(nextStats);
      setError(null);
      if (!nextNodes.some((node) => node.id === selectedIdRef.current)) {
        selectNodeId(nextNodes[0]?.id ?? null);
      }
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setLoading(false);
    }
  }

  async function loadAll() {
    try {
      const taskId = await loadTasks();
      if (taskId === null) {
        clearOverview();
        setLoading(false);
        return;
      }
      await loadOverview(taskId);
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadAll();
    const timer = window.setInterval(() => void loadAll(), 15000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const handlePopState = () => setPage(pageFromPath(window.location.pathname));
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  useEffect(() => {
    selectNodeId(null);
    setDetail(null);
    setHistories({});
    if (selectedTaskId === null) {
      clearOverview();
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadOverview(selectedTaskId);
  }, [selectedTaskId]);

  useEffect(() => {
    if (selectedId === null) {
      setDetail(null);
      setHistories({});
      return;
    }
    const nodeId = selectedId;
    async function loadDetail() {
      try {
        const nextDetail = await fetchNode(nodeId);
        const metricKeys = Array.from(
          new Set([...DEFAULT_CHART_METRICS.map((metric) => metric.key), ...Object.keys(nextDetail.metrics)])
        );
        const historyRows = await Promise.all(
          metricKeys.map(async (metric) => [metric, await fetchHistory(nodeId, metric, range)] as const)
        );
        setDetail(nextDetail);
        setHistories(Object.fromEntries(historyRows));
      } catch (exc) {
        setError(exc instanceof Error ? exc.message : String(exc));
      }
    }
    void loadDetail();
  }, [selectedId, range]);

  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    const rows = nodes.filter((node) => {
      const statusOk = statusFilter === "all" || node.status === statusFilter;
      const countryOk = countryFilter === "all" || node.meta?.country === countryFilter;
      const asnOk = asnFilter === "all" || node.meta?.asn === asnFilter;
      const searchOk =
        term.length === 0 ||
        node.name.toLowerCase().includes(term) ||
        (node.server ?? "").toLowerCase().includes(term) ||
        (node.meta?.exit_ip ?? "").toLowerCase().includes(term) ||
        (node.meta?.asn ?? "").toLowerCase().includes(term);
      return statusOk && countryOk && asnOk && searchOk;
    });
    return rows.sort((a, b) => {
      if (sortKey === "name") return a.name.localeCompare(b.name);
      if (sortKey === "status") return a.status.localeCompare(b.status);
      const left = a.metrics.delay?.latency_ms ?? Number.POSITIVE_INFINITY;
      const right = b.metrics.delay?.latency_ms ?? Number.POSITIVE_INFINITY;
      if (sortKey === "score") return (b.score ?? -1) - (a.score ?? -1);
      return left - right;
    });
  }, [nodes, asnFilter, countryFilter, search, sortKey, statusFilter]);

  const countries = useMemo(
    () => Array.from(new Set(nodes.map((node) => node.meta?.country).filter(isPresent))).sort(),
    [nodes]
  );
  const asns = useMemo(
    () => Array.from(new Set(nodes.map((node) => node.meta?.asn).filter(isPresent))).sort(),
    [nodes]
  );

  const selected = nodes.find((node) => node.id === selectedId) ?? null;

  async function handleTaskSubmit(values: { name: string; source_url: string; interval_seconds: number; advanced_probes_enabled: boolean }) {
    setSavingTask(true);
    try {
      const response = editingTask
        ? await updateTask(editingTask.id, values)
        : await createTask({ ...values, enabled: true });
      setShowForm(false);
      setEditingTask(null);
      await loadTasks(response.task.id);
      await loadOverview(response.task.id);
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setSavingTask(false);
    }
  }

  async function handleRunTask() {
    if (!selectedTaskId) return;
    setBusyTaskId(selectedTaskId);
    try {
      await runTask(selectedTaskId);
      await loadAll();
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusyTaskId(null);
    }
  }

  async function handleRunMiaoSpeedTask() {
    if (!selectedTaskId) return;
    setBusyTaskId(selectedTaskId);
    try {
      await runMiaoSpeedTask(selectedTaskId);
      await loadAll();
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusyTaskId(null);
    }
  }

  async function handleRefreshTask(taskId: number) {
    setBusyTaskId(taskId);
    try {
      await refreshTask(taskId);
      await loadTasks(taskId);
      await loadOverview(taskId);
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusyTaskId(null);
    }
  }

  async function handleToggleTask() {
    if (!selectedTask) return;
    setBusyTaskId(selectedTask.id);
    try {
      await updateTask(selectedTask.id, { enabled: !selectedTask.enabled });
      await loadAll();
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusyTaskId(null);
    }
  }

  async function handleDeleteTask() {
    if (!selectedTask) return;
    const confirmed = window.confirm(`删除监测任务「${selectedTask.name}」及其节点历史？`);
    if (!confirmed) return;
    setBusyTaskId(selectedTask.id);
    try {
      await deleteTask(selectedTask.id);
      const nextTaskId = await loadTasks(null);
      if (nextTaskId === null) {
        clearOverview();
      } else {
        await loadOverview(nextTaskId);
      }
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusyTaskId(null);
    }
  }

  const cancelForm = () => {
    setShowForm(false);
    setEditingTask(null);
  };

  return (
    <NetCoffeeShell page={page} onNavigate={navigate}>
      <TaskSelector
        tasks={tasks}
        selectedTask={selectedTask}
        selectedTaskId={selectedTaskId}
        busyTaskId={busyTaskId}
        onSelect={selectTaskId}
        onCreate={() => {
          setEditingTask(null);
          setShowForm(true);
        }}
        onEdit={() => selectedTask && (setEditingTask(selectedTask), setShowForm(true))}
        onRefresh={handleRefreshTask}
        onRun={handleRunTask}
        onToggle={handleToggleTask}
        onDelete={handleDeleteTask}
      />

      {showForm && <TaskForm task={editingTask} onSubmit={handleTaskSubmit} onCancel={cancelForm} saving={savingTask} />}
      {error && (
        <div className="error-banner">
          <AlertTriangle size={16} />
          {error}
        </div>
      )}

      {page === "monitor" && (
        <MonitorPage
          selectedTask={selectedTask}
          stats={stats}
          selected={selected}
          detail={detail}
          filtered={filtered}
          selectedId={selectedId}
          loading={loading}
          histories={histories}
          range={range}
          search={search}
          statusFilter={statusFilter}
          countryFilter={countryFilter}
          asnFilter={asnFilter}
          sortKey={sortKey}
          countries={countries}
          asns={asns}
          onSelectNode={(node) => selectNodeId(node.id)}
          onRangeChange={setRange}
          setSearch={setSearch}
          setStatusFilter={setStatusFilter}
          setCountryFilter={setCountryFilter}
          setAsnFilter={setAsnFilter}
          setSortKey={setSortKey}
        />
      )}

      {page === "ip" && (
        <IpProfilePage
          nodes={nodes}
          selected={selected}
          detail={detail}
          search={search}
          setSearch={setSearch}
          onSelectNode={(node) => selectNodeId(node.id)}
        />
      )}

      {page === "dns" && (
        <DnsLeakPage
          nodes={nodes}
          selectedTask={selectedTask}
          selected={selected}
          detail={detail}
          busyTaskId={busyTaskId}
          onRunQuick={handleRunTask}
          onRunDeep={handleRunMiaoSpeedTask}
          onSelectNode={(node) => selectNodeId(node.id)}
        />
      )}
    </NetCoffeeShell>
  );
}

createRoot(document.getElementById("root") as HTMLElement).render(<App />);
