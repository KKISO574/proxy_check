import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Gauge,
  RefreshCw,
  Search,
  Server,
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
  fetchHistory,
  fetchNode,
  fetchNodes,
  fetchStats,
  NodeDetail,
  NodeItem,
  ProbePoint,
  runTests,
  Stats
} from "./api";
import "./styles.css";

type StatusFilter = "all" | "available" | "down" | "unknown";
type RangeFilter = "1h" | "6h" | "24h" | "7d" | "30d";
type SortKey = "name" | "status" | "delay";

function formatLatency(value: number | null): string {
  if (value === null || Number.isNaN(value)) return "-";
  if (value >= 1000) return `${(value / 1000).toFixed(2)}s`;
  return `${Math.round(value)}ms`;
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

function EmptyState() {
  return (
    <div className="empty-state">
      <Server size={28} />
      <strong>暂无节点</strong>
      <span>配置 Clash/Mihomo YAML 后，平台会在下一轮检测时同步节点。</span>
    </div>
  );
}

function NodeTable({
  nodes,
  selectedId,
  onSelect
}: {
  nodes: NodeItem[];
  selectedId: number | null;
  onSelect: (node: NodeItem) => void;
}) {
  if (nodes.length === 0) return <EmptyState />;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>节点</th>
            <th>状态</th>
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
              <td>{formatLatency(node.latest_delay_ms)}</td>
              <td>
                <div>{formatLatency(node.latest_tcping_ms)}</div>
                <span className="muted">{node.latest_tcping_target ?? "-"}</span>
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

function ChartPanel({
  title,
  points,
  color
}: {
  title: string;
  points: ProbePoint[];
  color: string;
}) {
  const data = points.map((point) => ({
    time: formatTime(point.created_at),
    latency: point.success ? point.latency_ms : null,
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
              <CartesianGrid strokeDasharray="3 3" stroke="#d7dee8" />
              <XAxis dataKey="time" tick={{ fontSize: 11 }} minTickGap={24} />
              <YAxis tick={{ fontSize: 11 }} width={52} />
              <Tooltip
                contentStyle={{
                  border: "1px solid #d7dee8",
                  borderRadius: 6,
                  boxShadow: "0 10px 24px rgba(15, 23, 42, 0.12)"
                }}
              />
              <Line
                type="monotone"
                dataKey="latency"
                stroke={color}
                strokeWidth={2}
                dot={false}
                connectNulls={false}
                name="latency(ms)"
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </section>
  );
}

function DetailPane({
  selected,
  detail,
  delayHistory,
  tcpHistory,
  range,
  onRangeChange
}: {
  selected: NodeItem | null;
  detail: NodeDetail | null;
  delayHistory: ProbePoint[];
  tcpHistory: ProbePoint[];
  range: RangeFilter;
  onRangeChange: (range: RangeFilter) => void;
}) {
  if (!selected) {
    return (
      <aside className="detail-pane">
        <div className="detail-placeholder">选择一个节点查看历史折线和错误记录</div>
      </aside>
    );
  }

  return (
    <aside className="detail-pane">
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

      <ChartPanel title="真延迟" points={delayHistory} color="#2563eb" />
      <ChartPanel title="tcping" points={tcpHistory} color="#059669" />

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
    </aside>
  );
}

function App() {
  const [nodes, setNodes] = useState<NodeItem[]>([]);
  const [stats, setStats] = useState<Stats | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [detail, setDetail] = useState<NodeDetail | null>(null);
  const [delayHistory, setDelayHistory] = useState<ProbePoint[]>([]);
  const [tcpHistory, setTcpHistory] = useState<ProbePoint[]>([]);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [sortKey, setSortKey] = useState<SortKey>("delay");
  const [search, setSearch] = useState("");
  const [range, setRange] = useState<RangeFilter>("24h");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);

  async function loadOverview(autoSelect = false) {
    try {
      const [nextNodes, nextStats] = await Promise.all([fetchNodes(), fetchStats()]);
      setNodes(nextNodes);
      setStats(nextStats);
      setError(null);
      if (autoSelect && nextNodes.length > 0) {
        setSelectedId(nextNodes[0].id);
      }
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadOverview(true);
    const timer = window.setInterval(() => void loadOverview(), 15000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (selectedId === null && nodes.length > 0) {
      setSelectedId(nodes[0].id);
    }
  }, [nodes, selectedId]);

  useEffect(() => {
    if (selectedId === null) return;
    const nodeId = selectedId;
    async function loadDetail() {
      try {
        const [nextDetail, delay, tcping] = await Promise.all([
          fetchNode(nodeId),
          fetchHistory(nodeId, "delay", range),
          fetchHistory(nodeId, "tcping", range)
        ]);
        setDetail(nextDetail);
        setDelayHistory(delay);
        setTcpHistory(tcping);
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
      const searchOk =
        term.length === 0 ||
        node.name.toLowerCase().includes(term) ||
        (node.server ?? "").toLowerCase().includes(term);
      return statusOk && searchOk;
    });
    return rows.sort((a, b) => {
      if (sortKey === "name") return a.name.localeCompare(b.name);
      if (sortKey === "status") return a.status.localeCompare(b.status);
      const left = a.latest_delay_ms ?? Number.POSITIVE_INFINITY;
      const right = b.latest_delay_ms ?? Number.POSITIVE_INFINITY;
      return left - right;
    });
  }, [nodes, search, sortKey, statusFilter]);

  const selected = nodes.find((node) => node.id === selectedId) ?? null;

  async function handleRun() {
    setRunning(true);
    try {
      await runTests();
      await loadOverview();
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setRunning(false);
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div>
          <h1>Proxy Check</h1>
          <p>节点质量检测平台</p>
        </div>
        <button className="primary-button" onClick={handleRun} disabled={running}>
          <RefreshCw size={16} className={running ? "spin" : ""} />
          立即检测
        </button>
      </header>

      {error && (
        <div className="error-banner">
          <AlertTriangle size={16} />
          {error}
        </div>
      )}

      <main className="dashboard-grid">
        <section className="main-pane">
          <div className="metric-grid">
            <MetricCard icon={<Server size={20} />} label="节点总数" value={`${stats?.total_nodes ?? 0}`} />
            <MetricCard
              icon={<CheckCircle2 size={20} />}
              label="可用节点"
              value={`${stats?.available_nodes ?? 0}`}
              tone="ok"
            />
            <MetricCard
              icon={<XCircle size={20} />}
              label="异常节点"
              value={`${stats?.down_nodes ?? 0}`}
              tone="danger"
            />
            <MetricCard
              icon={<Gauge size={20} />}
              label="平均延迟"
              value={formatLatency(stats?.average_delay_ms ?? null)}
            />
          </div>

          <section className="list-panel">
            <div className="toolbar">
              <div className="search-box">
                <Search size={16} />
                <input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder="搜索节点或入口地址"
                />
              </div>
              <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as StatusFilter)}>
                <option value="all">全部状态</option>
                <option value="available">可用</option>
                <option value="down">异常</option>
                <option value="unknown">未知</option>
              </select>
              <select value={sortKey} onChange={(event) => setSortKey(event.target.value as SortKey)}>
                <option value="delay">按延迟</option>
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
              <NodeTable nodes={filtered} selectedId={selectedId} onSelect={(node) => setSelectedId(node.id)} />
            )}
          </section>
        </section>

        <DetailPane
          selected={selected}
          detail={detail}
          delayHistory={delayHistory}
          tcpHistory={tcpHistory}
          range={range}
          onRangeChange={setRange}
        />
      </main>
    </div>
  );
}

createRoot(document.getElementById("root") as HTMLElement).render(<App />);
