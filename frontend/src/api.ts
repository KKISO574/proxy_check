export type NodeStatus = "unknown" | "available" | "down" | "removed";

export interface NodeItem {
  id: number;
  task_id: number | null;
  name: string;
  type: string | null;
  server: string | null;
  port: number | null;
  listener_port: number | null;
  status: NodeStatus;
  metrics: Record<string, MetricSummary>;
  meta: NodeMeta | null;
  score: number | null;
  score_confidence: number;
  score_breakdown: Record<string, ScoreComponent>;
  last_checked_at: string | null;
}

export interface ScoreComponent {
  weight: number;
  score: number;
  contribution: number;
  value: number | null;
  status: string;
}

export interface NodeMeta {
  exit_ip: string | null;
  asn: string | null;
  country: string | null;
  region: string | null;
  isp: string | null;
  netflix_unlock: string | null;
  disney_unlock: string | null;
  openai_unlock: string | null;
  youtube_unlock: string | null;
  dns_leak: string | null;
}

export interface MetricSummary {
  metric: string;
  target: string;
  latency_ms: number | null;
  value: number | null;
  data: string | null;
  success: boolean;
  error: string | null;
  created_at: string;
}

export interface ProbePoint {
  created_at: string;
  metric: string;
  target: string;
  latency_ms: number | null;
  value: number | null;
  data: string | null;
  success: boolean;
  error: string | null;
}

export interface NodeDetail extends NodeItem {
  recent_errors: ProbePoint[];
}

export interface Stats {
  total_nodes: number;
  available_nodes: number;
  down_nodes: number;
  unknown_nodes: number;
  average_delay_ms: number | null;
}

export interface MonitorTask {
  id: number;
  name: string;
  source_url: string;
  enabled: boolean;
  interval_seconds: number;
  advanced_probes_enabled: boolean;
  status: NodeStatus;
  node_count: number;
  last_refresh_at: string | null;
  last_refresh_error: string | null;
  last_checked_at: string | null;
  next_run_at: string | null;
}

export interface TaskImportResponse {
  task: MonitorTask;
  nodes: number;
}

export interface TaskPayload {
  name: string;
  source_url: string;
  interval_seconds: number;
  enabled?: boolean;
  advanced_probes_enabled?: boolean;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json"
    },
    ...init
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Request failed: ${response.status}`);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

function withTask(path: string, taskId?: number | null): string {
  if (taskId === null || taskId === undefined) return path;
  const sep = path.includes("?") ? "&" : "?";
  return `${path}${sep}task_id=${taskId}`;
}

export function fetchTasks(): Promise<MonitorTask[]> {
  return request<MonitorTask[]>("/api/tasks");
}

export function createTask(payload: TaskPayload): Promise<TaskImportResponse> {
  return request<TaskImportResponse>("/api/tasks", {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export function updateTask(id: number, payload: Partial<TaskPayload>): Promise<TaskImportResponse> {
  return request<TaskImportResponse>(`/api/tasks/${id}`, {
    method: "PATCH",
    body: JSON.stringify(payload)
  });
}

export function deleteTask(id: number): Promise<void> {
  return request<void>(`/api/tasks/${id}`, { method: "DELETE" });
}

export function refreshTask(id: number): Promise<TaskImportResponse> {
  return request<TaskImportResponse>(`/api/tasks/${id}/refresh`, { method: "POST" });
}

export function runTask(id: number): Promise<{ nodes: number; results: number; errors: number }> {
  return request(`/api/tasks/${id}/run`, { method: "POST" });
}

export function fetchNodes(taskId?: number | null): Promise<NodeItem[]> {
  return request<NodeItem[]>(withTask("/api/nodes", taskId));
}

export function fetchNode(id: number): Promise<NodeDetail> {
  return request<NodeDetail>(`/api/nodes/${id}`);
}

export function fetchHistory(
  id: number,
  metric: string,
  range: "1h" | "6h" | "24h" | "7d" | "30d"
): Promise<ProbePoint[]> {
  return request<ProbePoint[]>(`/api/nodes/${id}/history?metric=${metric}&range=${range}`);
}

export function fetchStats(taskId?: number | null): Promise<Stats> {
  return request<Stats>(withTask("/api/stats", taskId));
}

export function runTests(): Promise<{ nodes: number; results: number; errors: number }> {
  return request("/api/tests/run", { method: "POST" });
}
