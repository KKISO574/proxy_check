export type NodeStatus = "unknown" | "available" | "down" | "removed";

export interface NodeItem {
  id: number;
  name: string;
  type: string | null;
  server: string | null;
  port: number | null;
  listener_port: number | null;
  status: NodeStatus;
  latest_delay_ms: number | null;
  latest_tcping_ms: number | null;
  latest_tcping_target: string | null;
  last_checked_at: string | null;
}

export interface ProbePoint {
  created_at: string;
  metric: "delay" | "tcping";
  target: string;
  latency_ms: number | null;
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
  return response.json() as Promise<T>;
}

export function fetchNodes(): Promise<NodeItem[]> {
  return request<NodeItem[]>("/api/nodes");
}

export function fetchNode(id: number): Promise<NodeDetail> {
  return request<NodeDetail>(`/api/nodes/${id}`);
}

export function fetchHistory(
  id: number,
  metric: "delay" | "tcping",
  range: "1h" | "6h" | "24h" | "7d" | "30d"
): Promise<ProbePoint[]> {
  return request<ProbePoint[]>(`/api/nodes/${id}/history?metric=${metric}&range=${range}`);
}

export function fetchStats(): Promise<Stats> {
  return request<Stats>("/api/stats");
}

export function runTests(): Promise<{ nodes: number; results: number; errors: number }> {
  return request("/api/tests/run", { method: "POST" });
}

