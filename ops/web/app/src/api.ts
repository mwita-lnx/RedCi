// Thin fetch wrapper for the panel JSON API. Same-origin cookies carry the
// session; filippo.io/csrf accepts same-origin requests without a token.

const BASE = "/api/v1";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function handle<T>(res: Response): Promise<T> {
  if (res.status === 401) {
    // Session gone — bounce to the server-rendered login.
    window.location.href = "/login";
    throw new ApiError(401, "unauthorized");
  }
  const text = await res.text();
  const body = text ? JSON.parse(text) : null;
  if (!res.ok) {
    throw new ApiError(res.status, body?.error || res.statusText);
  }
  return body as T;
}

export function apiGet<T>(path: string): Promise<T> {
  return fetch(BASE + path, { credentials: "include" }).then((r) => handle<T>(r));
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return fetch(BASE + path, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  }).then((r) => handle<T>(r));
}

export function apiDelete<T>(path: string): Promise<T> {
  return fetch(BASE + path, { method: "DELETE", credentials: "include" }).then((r) => handle<T>(r));
}

// ---- types ----

export interface Me { id: number; email: string; role: string }

export interface Dashboard {
  servers: number; servers_online: number; servers_offline: number;
  sites: number; web_apps: number; running_jobs: number;
  certs_expiring: number; disks_low: number;
}

export interface Site {
  id: number; domain: string; status: string; ssl: boolean; bench_id: number;
  apps?: AppInfo[];
}
export interface AppInfo { name: string; version: string; rollbackable: boolean }

export interface Bench { id: number; name: string; path: string; server_id: number }

export interface Deploy {
  id: number; kind: string; target_type: string; target_id: number;
  trigger: string; status: string; error: string; created_at: number; last_job_id: number;
}

export interface JobLog { seq: number; stream: string; line: string; ts: number }
export interface Job {
  id: number; type: string; status: string; error: string; logs: JobLog[];
  started?: number; finished?: number;
  deploy_id?: number; kind?: string; commit?: string; trigger?: string;
  target_type?: string; target_id?: number; created_at?: number;
}

export interface Server {
  id: number; name: string; hostname: string; status: string; agent_version: string;
}

export interface AppSource {
  id: number; name: string; repo: string; branch: string; kind: string; auto_deploy: boolean;
  latest_commit?: string;
}

export interface BenchFacts {
  python_version?: string; node_version?: string; db_type?: string; db_host?: string;
  web_workers?: number; rq_workers?: number; scheduler_on?: boolean; queues?: Record<string, number>;
}
export interface BenchCard {
  id: number; name: string; path: string; server_id: number; server_name: string;
  server_status: string; frappe_version: string; env: string; sites: number;
  apps: { name: string; version: string; branch: string; latest_commit: string }[]; facts: BenchFacts;
}
export interface MatrixRow {
  site_id: number; site: string; bench_id: number; bench: string; env: string;
  status: string; last_backup: number; apps: string[]; versions: Record<string, string>;
  branches: Record<string, string>; latest_commits: Record<string, string>;
}
export interface BenchesOverview {
  benches: BenchCard[]; columns: string[]; matrix: MatrixRow[];
  backups: { site: string; at: number }[];
}

export interface FleetServer {
  id: number; name: string; hostname: string; status: string; online: boolean;
  role: string; agent_version: string; cpus: number; mem_total_mb: number;
  cpu_pct: number; mem_pct: number; disk_pct: number; os: string;
  benches: number; uptime_days: number; last_seen: number;
}
export interface Fleet {
  servers: FleetServer[]; online: number; total: number;
  total_cpu: number; total_mem_mb: number; used_mem_mb: number; running_jobs: number;
  agent_versions: { version: string; count: number; latest: boolean }[];
  latest_agent: string;
  attention: { level: string; server: string; title: string; detail: string }[];
}

export interface GithubStatus { connected: boolean; login?: string; error?: string }
export interface GhRepo { full_name: string; private: boolean; default_branch: string }
export interface GhBranch { name: string; sha: string }
export interface GhCommit { sha: string; message: string; author: string; date: string }
export interface WebApp { id: number; name: string; internal_port: number; status: string; commit: string }
export interface Route { id: number; domain: string; upstream: string; ssl: boolean; status: string }
export interface User { id: number; email: string; role: string; disabled: boolean }
export interface Audit {
  id: number; actor: string; action: string; object_type: string;
  object_id: number; detail: string; created_at: number;
}

export interface PipelineEnv {
  id: number; name: string; rank: number; site_id: number; site_domain: string;
  site_status: string; require_approval: boolean; current_commit: string;
  last_deploy_id: number; server_id: number; server_name: string;
  agent_online: boolean; status: string;
}
export interface TimelineHop {
  env: string; reached: boolean; status?: string; run?: number; by?: string; at?: number;
}
export interface TimelineChange { commit: string; updated_at: number; hops: TimelineHop[] }
export interface Pipeline {
  id: number; name: string; app: string; app_source_id: number;
  created_at: number; envs: PipelineEnv[]; timeline?: TimelineChange[];
}

// role ranking matching the Go auth.Role ladder.
const ROLE_RANK: Record<string, number> = { viewer: 1, deployer: 2, admin: 3 };
export function atLeast(role: string, min: string): boolean {
  return (ROLE_RANK[role] ?? 0) >= (ROLE_RANK[min] ?? 99);
}
