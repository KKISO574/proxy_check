import React, { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CirclePause,
  Gauge,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  Trash2,
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
  runTask,
  Stats,
  updateTask
} from "./api";
import "./styles.css";

type StatusFilter = "all" | "available" | "down" | "unknown";
type RangeFilter = "1h" | "6h" | "24h" | "7d" | "30d";
type SortKey = "name" | "status" | "delay";
type ChartMetric = {
  key: string;
  label: string;
  color: string;
};

const DEFAULT_CHART_METRICS: ChartMetric[] = [
  { key: "delay", label: "真延迟", color: "#16a34a" },
  { key: "tcping", label: "tcping", color: "#2563eb" }
];

function isPresent(value: string | null | undefined): value is string {
  return Boolean(value);
}

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

function IconButton({
  label,
  children,
  onClick,
  disabled,
  tone
}: {
  label: string;
  children: React.ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  tone?: "danger";
}) {
  return (
    <button
      className={`icon-button ${tone ?? ""}`}
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </button>
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

function TaskSidebar({
  tasks,
  selectedTaskId,
  onSelect,
  onCreate,
  onRefresh,
  busyTaskId
}: {
  tasks: MonitorTask[];
  selectedTaskId: number | null;
  onSelect: (id: number) => void;
  onCreate: () => void;
  onRefresh: (id: number) => void;
  busyTaskId: number | null;
}) {
  return (
    <aside className="task-sidebar">
      <div className="brand">
        <div className="brand-mark">
          <Activity size={20} />
        </div>
        <div>
          <h1>Proxy Check</h1>
          <p>节点质量检测平台</p>
        </div>
      </div>
      <button className="add-task" type="button" onClick={onCreate}>
        <Plus size={16} />
        导入配置 URL
      </button>
      <div className="task-list">
        {tasks.length === 0 ? (
          <div className="task-empty">暂无监测任务</div>
        ) : (
          tasks.map((task) => (
            <button
              className={`task-item ${selectedTaskId === task.id ? "active" : ""}`}
              key={task.id}
              type="button"
              onClick={() => onSelect(task.id)}
            >
              <span className={`task-dot dot-${task.enabled ? task.status : "paused"}`} />
              <span className="task-copy">
                <strong>{task.name}</strong>
                <small>
                  {task.enabled ? statusLabel(task.status) : "已暂停"} · {task.node_count} 节点
                </small>
              </span>
              <span
                className="task-refresh"
                title="刷新配置"
                onClick={(event) => {
                  event.stopPropagation();
                  onRefresh(task.id);
                }}
              >
                <RefreshCw size={14} className={busyTaskId === task.id ? "spin" : ""} />
              </span>
            </button>
          ))
        )}
      </div>
    </aside>
  );
}

function TaskForm({
  task,
  onSubmit,
  onCancel,
  saving
}: {
  task: MonitorTask | null;
  onSubmit: (values: { name: string; source_url: string; interval_seconds: number }) => Promise<void>;
  onCancel: () => void;
  saving: boolean;
}) {
  const [name, setName] = useState(task?.name ?? "");
  const [sourceUrl, setSourceUrl] = useState(task?.source_url ?? "");
  const [interval, setInterval] = useState(`${task?.interval_seconds ?? 60}`);

  useEffect(() => {
    setName(task?.name ?? "");
    setSourceUrl(task?.source_url ?? "");
    setInterval(`${task?.interval_seconds ?? 60}`);
  }, [task]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    await onSubmit({
      name: name.trim(),
      source_url: sourceUrl.trim(),
      interval_seconds: Math.max(10, Number(interval) || 60)
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
                <div>{node.meta?.country ?? "-"}</div>
                <span className="muted">{node.meta?.asn ?? "-"}</span>
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

function ChartPanel({ title, points, color }: { title: string; points: ProbePoint[]; color: string }) {
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
        label: key,
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

          <MetaPanel detail={detail} />

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
  const selectedTaskIdRef = useRef<number | null>(selectedTaskId);
  const selectedIdRef = useRef<number | null>(selectedId);

  const selectedTask = tasks.find((task) => task.id === selectedTaskId) ?? null;

  function selectTaskId(taskId: number | null) {
    selectedTaskIdRef.current = taskId;
    setSelectedTaskId(taskId);
  }

  function selectNodeId(nodeId: number | null) {
    selectedIdRef.current = nodeId;
    setSelectedId(nodeId);
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
    selectNodeId(null);
    setDetail(null);
    setHistories({});
    setLoading(true);
    void loadOverview(selectedTaskId);
  }, [selectedTaskId]);

  useEffect(() => {
    if (selectedId === null) return;
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
      const left = a.latest_delay_ms ?? Number.POSITIVE_INFINITY;
      const right = b.latest_delay_ms ?? Number.POSITIVE_INFINITY;
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

  async function handleTaskSubmit(values: { name: string; source_url: string; interval_seconds: number }) {
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
      await loadTasks(null);
      await loadOverview(null);
    } catch (exc) {
      setError(exc instanceof Error ? exc.message : String(exc));
    } finally {
      setBusyTaskId(null);
    }
  }

  return (
    <div className="app-layout">
      <TaskSidebar
        tasks={tasks}
        selectedTaskId={selectedTaskId}
        onSelect={selectTaskId}
        onCreate={() => {
          setEditingTask(null);
          setShowForm(true);
        }}
        onRefresh={handleRefreshTask}
        busyTaskId={busyTaskId}
      />

      <main className="workspace">
        <header className="workspace-header">
          <div>
            <span className="eyebrow">Monitor Task</span>
            <h2>{selectedTask?.name ?? "未选择任务"}</h2>
            <p>{selectedTask?.source_url ?? "导入 Clash/Mihomo YAML URL 后开始同步节点。"}</p>
          </div>
          <div className="header-actions">
            <IconButton label="编辑任务" onClick={() => selectedTask && (setEditingTask(selectedTask), setShowForm(true))} disabled={!selectedTask}>
              <Pencil size={16} />
            </IconButton>
            <IconButton label={selectedTask?.enabled ? "暂停任务" : "启用任务"} onClick={handleToggleTask} disabled={!selectedTask}>
              <CirclePause size={16} />
            </IconButton>
            <IconButton label="删除任务" onClick={handleDeleteTask} disabled={!selectedTask} tone="danger">
              <Trash2 size={16} />
            </IconButton>
            <button className="primary-button" onClick={handleRunTask} disabled={!selectedTask || busyTaskId === selectedTask.id}>
              <RefreshCw size={16} className={busyTaskId === selectedTask?.id ? "spin" : ""} />
              检测任务
            </button>
          </div>
        </header>

        {showForm && (
          <TaskForm
            task={editingTask}
            onSubmit={handleTaskSubmit}
            onCancel={() => {
              setShowForm(false);
              setEditingTask(null);
            }}
            saving={savingTask}
          />
        )}

        {error && (
          <div className="error-banner">
            <AlertTriangle size={16} />
            {error}
          </div>
        )}

        <div className="metric-grid">
          <MetricCard icon={<Server size={20} />} label="节点总数" value={`${stats?.total_nodes ?? 0}`} />
          <MetricCard icon={<CheckCircle2 size={20} />} label="可用节点" value={`${stats?.available_nodes ?? 0}`} tone="ok" />
          <MetricCard icon={<XCircle size={20} />} label="异常节点" value={`${stats?.down_nodes ?? 0}`} tone="danger" />
          <MetricCard icon={<Gauge size={20} />} label="平均延迟" value={formatLatency(stats?.average_delay_ms ?? null)} />
        </div>

        <section className="content-grid">
          <section className="list-panel">
            <div className="toolbar">
              <div className="search-box">
                <Search size={16} />
                <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索节点或入口地址" />
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
              <NodeTable nodes={filtered} selectedId={selectedId} onSelect={(node) => selectNodeId(node.id)} hasTask={Boolean(selectedTask)} />
            )}
          </section>

          <DetailPane
            selected={selected}
            detail={detail}
            histories={histories}
            range={range}
            onRangeChange={setRange}
          />
        </section>
      </main>
    </div>
  );
}

createRoot(document.getElementById("root") as HTMLElement).render(<App />);
