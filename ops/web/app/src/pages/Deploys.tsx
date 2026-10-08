import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, Deploy } from "../api";
import { Badge, Empty, PanelBox, TableSkeleton } from "../ui";

const RUNNING = ["running", "queued", "ready", "claimed"];
function ago(ts: number) {
  const s = Math.floor(Date.now() / 1000 - ts);
  if (s < 60) return "just now"; if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`; return `${Math.floor(s / 86400)}d ago`;
}
function hhmm(ts: number) { return new Date(ts * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }); }
function dayKey(ts: number) {
  const d = new Date(ts * 1000); d.setHours(0, 0, 0, 0);
  const today = new Date(); today.setHours(0, 0, 0, 0);
  const diff = Math.round((today.getTime() - d.getTime()) / 86400000);
  const label = d.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "short" });
  return diff === 0 ? `Today · ${label.replace(/^\w+, /, "")}` : diff === 1 ? `Yesterday · ${label.replace(/^\w+, /, "")}` : label;
}
function stageBars(status: string) {
  return status === "succeeded" ? ["ok", "ok", "ok", "ok", "ok"] :
    status === "failed" ? ["ok", "ok", "fail", "pend", "pend"] :
    status === "running" ? ["ok", "run", "pend", "pend", "pend"] :
    status === "cancelled" ? ["ok", "pend", "pend", "pend", "pend"] :
    ["pend", "pend", "pend", "pend", "pend"];
}
type Filter = "all" | "passed" | "failed" | "running" | "cancelled";
const matchFilter = (f: Filter, s: string) =>
  f === "all" ? true :
  f === "passed" ? s === "succeeded" :
  f === "failed" ? s === "failed" || s === "lost" :
  f === "running" ? RUNNING.includes(s) :
  s === "cancelled";

export function DeploysPage() {
  const { data, isLoading } = useQuery({
    queryKey: ["deploys"], queryFn: () => apiGet<Deploy[]>("/deploys"),
    refetchInterval: (q) => (q.state.data as Deploy[] | undefined)?.some((d) => RUNNING.includes(d.status)) ? 2500 : 9000,
  });
  const [filter, setFilter] = useState<Filter>("all");
  const [q, setQ] = useState("");

  const rows = data ?? [];
  const running = rows.filter((d) => d.status === "running");
  const queued = rows.filter((d) => ["queued", "ready", "claimed"].includes(d.status));
  const live = [...running, ...queued].slice(0, 3);

  const counts = useMemo(() => ({
    all: rows.length,
    passed: rows.filter((d) => d.status === "succeeded").length,
    failed: rows.filter((d) => d.status === "failed" || d.status === "lost").length,
    running: running.length,
    cancelled: rows.filter((d) => d.status === "cancelled").length,
  }), [rows, running.length]);

  const filtered = rows.filter((d) => matchFilter(filter, d.status) &&
    (!q || `${d.kind} ${d.id} ${d.trigger} ${d.target_type}`.toLowerCase().includes(q.toLowerCase())));

  // group by day (already newest-first from API)
  const groups = useMemo(() => {
    const m = new Map<string, Deploy[]>();
    for (const d of filtered) {
      const k = dayKey(d.created_at);
      if (!m.has(k)) m.set(k, []);
      m.get(k)!.push(d);
    }
    return [...m.entries()];
  }, [filtered]);

  // week stats
  const stats = useMemo(() => {
    const finished = rows.filter((d) => d.status === "succeeded" || d.status === "failed");
    const passed = finished.filter((d) => d.status === "succeeded").length;
    return { success: finished.length ? ((passed / finished.length) * 100).toFixed(1) : "—", total: rows.length };
  }, [rows]);

  // per-day chart (last 7 days): ok vs fail counts
  const perDay = useMemo(() => {
    const now = new Date(); now.setHours(0, 0, 0, 0);
    const days: { label: string; ok: number; fail: number }[] = [];
    for (let k = 6; k >= 0; k--) {
      const day = new Date(now); day.setDate(now.getDate() - k);
      const start = Math.floor(day.getTime() / 1000), end = start + 86400;
      const inDay = rows.filter((d) => d.created_at >= start && d.created_at < end);
      days.push({
        label: day.toLocaleDateString(undefined, { weekday: "short" }),
        ok: inDay.filter((d) => d.status !== "failed" && d.status !== "lost").length,
        fail: inDay.filter((d) => d.status === "failed" || d.status === "lost").length,
      });
    }
    return days;
  }, [rows]);
  const maxDay = Math.max(1, ...perDay.map((d) => d.ok + d.fail));

  // failure hotspots: group failed runs by kind
  const hotspots = useMemo(() => {
    const m = new Map<string, number>();
    for (const d of rows) if (d.status === "failed" || d.status === "lost") m.set(d.kind, (m.get(d.kind) ?? 0) + 1);
    return [...m.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4);
  }, [rows]);
  const maxFail = Math.max(1, ...hotspots.map((h) => h[1]));

  return (
    <div className="stack">
      <div>
        <div className="rt-eyebrow">RUNS · LAST 7 DAYS · {stats.total} RUNS</div>
        <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
          <div className="rt-title" style={{ fontSize: 40 }}>Every run.<br /><span style={{ color: "var(--brand)" }}>Found in seconds.</span></div>
          <div style={{ display: "flex", gap: 10 }}>
            <button className="btn secondary" onClick={() => exportCSV(rows)}>Export CSV</button>
            <Link className="btn" to="/pipelines">▷ Run pipeline</Link>
          </div>
        </div>
      </div>

      {/* live now */}
      {live.length > 0 && (
        <div className="stack" style={{ gap: 12 }}>
          <div className="live-head"><span className="pulse" />Live now <span className="live-sub">{running.length} running · {queued.length} queued</span></div>
          <div className="live-cards">
            {live.map((d) => (
              <Link key={d.id} to={d.last_job_id ? `/jobs/${d.last_job_id}` : "/deploys"} className="live-card">
                <div className="lc-top"><span className="lc-name">{d.kind} <span className="num">#{d.id}</span></span><span className="lc-time">{ago(d.created_at)}</span></div>
                <div className="lc-msg">{d.target_type}:{d.target_id} · {d.trigger}</div>
                <div className="stagebars">{stageBars(d.status).map((c, i) => <i key={i} className={c} />)}</div>
                <div className="lc-foot"><span>{d.status}</span><span>run #{d.last_job_id || "—"}</span></div>
              </Link>
            ))}
          </div>
        </div>
      )}

      <div className="grid-2">
        <PanelBox>
          <div className="filterbar">
            <div className="search">
              <span className="mag">⌕</span>
              <input type="text" placeholder="Kind, run #, trigger…" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            <button className="selbtn">Pipeline ▾</button>
            <button className="selbtn">Trigger ▾</button>
            <button className="selbtn">Last 7 days ▾</button>
          </div>
          <div className="filter-row2">
            <div className="rtabs">
              {(["all", "passed", "failed", "running", "cancelled"] as Filter[]).map((f) => (
                <button key={f} className={filter === f ? "on" : ""} onClick={() => setFilter(f)}>
                  {f[0].toUpperCase() + f.slice(1)} <span className="cnt">{counts[f]}</span>
                </button>
              ))}
            </div>
          </div>

          {isLoading ? <TableSkeleton cols={6} /> : filtered.length === 0 ? <Empty>No runs match.</Empty> : (
            <table>
              <thead><tr><th>Status</th><th>Run</th><th>Trigger</th><th>Stages</th><th>Started</th><th></th></tr></thead>
              <tbody>
                {groups.map(([day, items]) => (
                  <RunGroup key={day} day={day} items={items} />
                ))}
              </tbody>
            </table>
          )}
          <div style={{ padding: "12px 18px", fontSize: 12.5, color: "var(--text-muted)" }}>Showing {filtered.length} of {rows.length} runs</div>
        </PanelBox>

        <div className="stack">
          <PanelBox title="This week">
            <div className="week-grid">
              <div className="week-stat"><div className="ws-k">Success</div><div className="ws-v" style={{ color: "var(--ok)" }}>{stats.success}{stats.success !== "—" && "%"}</div></div>
              <div className="week-stat"><div className="ws-k">Total runs</div><div className="ws-v">{stats.total}</div></div>
              <div className="week-stat"><div className="ws-k">Running</div><div className="ws-v">{counts.running}</div></div>
              <div className="week-stat"><div className="ws-k">Failed</div><div className="ws-v" style={{ color: counts.failed ? "var(--danger)" : undefined }}>{counts.failed}</div></div>
            </div>
            <div className="week-chart">
              <div className="wc-label">Runs per day · failures in red</div>
              <div className="week-bars">
                {perDay.map((d, i) => (
                  <div key={i} className={`wb ${i === perDay.length - 1 ? "today" : ""}`}>
                    {d.fail > 0 && <span className="wb-fail" style={{ height: `${(d.fail / maxDay) * 60}px` }} />}
                    <span className="wb-ok" style={{ height: `${(d.ok / maxDay) * 60}px`, minHeight: d.ok ? 3 : 0 }} />
                  </div>
                ))}
              </div>
              <div className="week-axis"><span>{perDay[0]?.label}</span><span>Today</span></div>
            </div>
          </PanelBox>

          <PanelBox title="Failure hotspots">
            {hotspots.length === 0 ? <div className="empty">No failures. Nice.</div> : (
              <div className="hotspots">
                {hotspots.map(([kind, n]) => (
                  <div className="hotspot" key={kind}>
                    <div className="hs-top"><span className="hs-name">{kind}</span><span className="hs-fails">{n} fail{n === 1 ? "" : "s"}</span></div>
                    <div className="hs-bar"><i style={{ width: `${(n / maxFail) * 100}%` }} /></div>
                  </div>
                ))}
              </div>
            )}
          </PanelBox>

          <PanelBox title="Saved views">
            <div className="saved-views">
              <div className="sv" onClick={() => setFilter("failed")}><span className="sv-name">Failed runs</span><span className="sv-count">{counts.failed}</span></div>
              <div className="sv" onClick={() => setFilter("running")}><span className="sv-name">In progress</span><span className="sv-count">{counts.running}</span></div>
              <div className="sv" onClick={() => setFilter("all")}><span className="sv-name">All runs</span><span className="sv-count">{counts.all}</span></div>
              <div className="sv-add" onClick={() => toast.message("Saved views coming soon")}>Save current filters</div>
            </div>
          </PanelBox>
        </div>
      </div>
    </div>
  );
}

function RunGroup({ day, items }: { day: string; items: Deploy[] }) {
  const nav = useNavigate();
  const fails = items.filter((d) => d.status === "failed" || d.status === "lost").length;
  const openRun = (d: Deploy) => {
    if (d.last_job_id > 0) nav(`/jobs/${d.last_job_id}`);
    else toast.message("This run has no job yet", { description: "It hasn't started on an agent." });
  };
  return (
    <>
      <tr className="day-group"><td colSpan={6}>{day}<span className="dg-aux">{items.length} runs{fails ? ` · ${fails} failed` : ""}</span></td></tr>
      {items.map((d) => (
        <tr key={d.id} className="run-row" style={{ cursor: "pointer" }} onClick={() => openRun(d)}>
          <td><Badge status={d.status === "succeeded" ? "succeeded" : d.status} /></td>
          <td>
            <div className="run-primary">{d.kind} <span className="num">#{d.id}</span></div>
            {d.error ? <div className="run-sub" style={{ color: "var(--danger)" }}>{d.error}</div> : <div className="run-sub">{d.target_type}:{d.target_id}</div>}
          </td>
          <td className="branch">{d.trigger}</td>
          <td><div className="stagebars">{stageBars(d.status).map((c, i) => <i key={i} className={c} />)}</div></td>
          <td className="cell-time">{hhmm(d.created_at)}</td>
          <td className="row-actions" onClick={(e) => e.stopPropagation()}>
            {d.last_job_id > 0 && <Link className="rerun-ic" to={`/jobs/${d.last_job_id}`} title="View logs">↻</Link>}
          </td>
        </tr>
      ))}
    </>
  );
}

function exportCSV(rows: Deploy[]) {
  const header = "id,kind,target,trigger,status,created_at\n";
  const body = rows.map((d) => `${d.id},${d.kind},${d.target_type}:${d.target_id},${d.trigger},${d.status},${new Date(d.created_at * 1000).toISOString()}`).join("\n");
  const blob = new Blob([header + body], { type: "text/csv" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a"); a.href = url; a.download = "redci-runs.csv"; a.click();
  URL.revokeObjectURL(url);
}
