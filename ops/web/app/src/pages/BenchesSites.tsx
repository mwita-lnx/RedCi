import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, atLeast, BenchCard, BenchesOverview, MatrixRow } from "../api";
import { Empty, PanelBox, TableSkeleton } from "../ui";

function ago(ts: number) {
  if (!ts) return "—";
  const s = Math.floor(Date.now() / 1000 - ts);
  if (s < 60) return "now"; if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`; return `${Math.floor(s / 86400)}d ago`;
}

type Filter = "all" | "production" | "staging" | "preview";

export function BenchesSitesPage({ role }: { role?: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["benches-overview"], queryFn: () => apiGet<BenchesOverview>("/benches/overview"), refetchInterval: 15_000,
  });
  const [filter, setFilter] = useState<Filter>("all");
  const canAdmin = atLeast(role ?? "", "admin");

  const benches = data?.benches ?? [];
  const totalSites = benches.reduce((a, b) => a + b.sites, 0);
  const matrix = (data?.matrix ?? []).filter((r) => filter === "all" || r.env === filter);

  return (
    <div className="stack">
      <div>
        <div className="rt-eyebrow">FRAPPE · {benches.length} BENCHES · {totalSites} SITES</div>
        <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
          <div className="rt-title" style={{ fontSize: 40 }}>
            Every bench, every site,<br /><span style={{ color: "var(--brand)" }}>every app version.</span>
          </div>
          <div style={{ display: "flex", gap: 10 }}>
            {canAdmin && <Link className="btn secondary" to="/servers">Connect a bench</Link>}
            {canAdmin && <Link className="btn" to="/sites">+ New site</Link>}
          </div>
        </div>
      </div>

      {/* bench cards */}
      {isLoading ? (
        <div className="bench-grid">{[0, 1, 2].map((i) => <div key={i} className="bench-card"><div className="skeleton" style={{ height: 180 }} /></div>)}</div>
      ) : benches.length === 0 ? (
        <PanelBox><Empty>No benches yet. Connect a server, refresh and import its benches.</Empty></PanelBox>
      ) : (
        <div className="bench-grid">
          {benches.map((b) => <BenchCardView key={b.id} b={b} />)}
        </div>
      )}

      {/* sites × apps matrix + side panels */}
      <div className="grid-2">
        <PanelBox
          title="Sites × apps"
          action={
            <div className="rtabs">
              {(["all", "production", "staging", "preview"] as Filter[]).map((f) => (
                <button key={f} className={filter === f ? "on" : ""} onClick={() => setFilter(f)}>
                  {f === "all" ? "All" : f[0].toUpperCase() + f.slice(1)}
                </button>
              ))}
            </div>
          }
        >
          {isLoading ? <TableSkeleton cols={6} /> : matrix.length === 0 ? <Empty>No sites.</Empty> : (
            <div className="matrix-wrap">
              <table className="matrix">
                <thead>
                  <tr>
                    <th>Site</th>
                    {(data?.columns ?? []).map((c) => <th key={c}>{c}</th>)}
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {matrix.map((r) => <MatrixRowView key={r.site} r={r} cols={data?.columns ?? []} />)}
                </tbody>
              </table>
            </div>
          )}
        </PanelBox>

        <div className="stack">
          <JobsPanel benches={benches} />
          <BackupsPanel backups={data?.backups ?? []} />
        </div>
      </div>
    </div>
  );
}

function BenchCardView({ b }: { b: BenchCard }) {
  const f = b.facts || {};
  const healthy = b.server_status === "online";
  return (
    <div className="bench-card">
      <div className="bc-top">
        <div>
          <div className="bc-name">{b.name}</div>
          <div className="bc-sub">{b.env ? b.env[0].toUpperCase() + b.env.slice(1) : "Bench"} · {b.sites} site{b.sites === 1 ? "" : "s"} · {b.server_name}</div>
        </div>
        <span className={`badge ${healthy ? "healthy" : "offline"}`}>{healthy ? "Healthy" : b.server_status}</span>
      </div>

      <div className="bc-apps">
        {b.apps.length === 0 ? <span className="muted" style={{ fontSize: 12 }}>no apps recorded</span> :
          b.apps.map((a) => <span key={a.name} className="apptag">{a.name} <b>{a.version || "—"}</b></span>)}
      </div>

      <div className="bc-facts">
        <div className="bc-fact"><div className="fk">Runtime</div><div className="fv">{f.python_version ? `Python ${f.python_version}` : "—"}{f.node_version ? ` · Node ${f.node_version}` : ""}</div></div>
        <div className="bc-fact"><div className="fk">Database</div><div className="fv">{f.db_type ? f.db_type : "—"}</div></div>
        <div className="bc-fact"><div className="fk">Workers</div><div className="fv">{(f.web_workers || f.rq_workers) ? `${f.web_workers ?? 0} web · ${f.rq_workers ?? 0} rq` : "—"}</div></div>
        <div className="bc-fact"><div className="fk">Scheduler</div><div className="fv">{f.scheduler_on === undefined ? "—" : f.scheduler_on ? "On" : "Off"}</div></div>
      </div>

      <div className={`bc-note ${healthy ? "ok" : ""}`}>{b.path}</div>
    </div>
  );
}

function MatrixRowView({ r, cols }: { r: MatrixRow; cols: string[] }) {
  return (
    <tr>
      <td className="site-cell">
        {r.site}
        <div className="sub">{r.bench} · backup {ago(r.last_backup)}</div>
      </td>
      {cols.map((c) => {
        const v = r.versions[c];
        const installed = c in r.versions;
        return (
          <td key={c}>
            {installed
              ? <span className={`vtag ${r.env === "staging" || r.env === "preview" ? "new" : ""}`}>{v || "—"}</span>
              : <span className="vtag none">—</span>}
          </td>
        );
      })}
      <td><span className={`badge ${r.status === "active" ? "active" : r.status}`}>{r.status === "active" ? "Live" : r.status}</span></td>
    </tr>
  );
}

// Background jobs: aggregate RQ queue depths across benches (from facts).
function JobsPanel({ benches }: { benches: BenchCard[] }) {
  const agg: Record<string, number> = {};
  let haveData = false;
  for (const b of benches) {
    const q = b.facts?.queues;
    if (q) { haveData = true; for (const [k, v] of Object.entries(q)) agg[k] = (agg[k] ?? 0) + v; }
  }
  const names = ["default", "short", "long"];
  const max = Math.max(1, ...names.map((n) => agg[n] ?? 0));
  return (
    <PanelBox title="Background jobs" action={<span className="muted" style={{ fontSize: 12 }}>{benches[0]?.name ?? ""}</span>}>
      <div className="queue">
        {names.map((n) => (
          <div className="q" key={n}>
            <div className="q-top"><span className="q-name">{n}</span><span className="q-count">{haveData ? `${agg[n] ?? 0} queued` : "—"}</span></div>
            <div className="q-bar"><i className={n} style={{ width: `${((agg[n] ?? 0) / max) * 100}%` }} /></div>
          </div>
        ))}
        <div className="q-foot">
          {haveData ? "Queue depths from the latest server status." : "Queue depths appear after a server refresh collects them."}
        </div>
      </div>
    </PanelBox>
  );
}

function BackupsPanel({ backups }: { backups: { site: string; at: number }[] }) {
  const recent = [...backups].sort((a, b) => b.at - a.at).slice(0, 6);
  return (
    <PanelBox title="Backups">
      <div className="backups">
        <div className="intro">Each deploy runs <b>bench backup</b> first. Most recent per site:</div>
        {recent.length === 0 ? <div className="muted" style={{ fontSize: 13, padding: "6px 0" }}>No backups recorded yet.</div> :
          recent.map((b) => (
            <div className="bk" key={b.site}>
              <span className="bk-site">{b.site}</span>
              <span className="bk-meta">{ago(b.at)}</span>
            </div>
          ))}
      </div>
    </PanelBox>
  );
}
