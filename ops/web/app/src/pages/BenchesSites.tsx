import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, atLeast, BenchCard, BenchesOverview, MatrixRow } from "../api";
import { Button, Empty, PanelBox } from "../ui";
import { NewSiteModal } from "../components/NewSiteModal";

function ago(ts: number) {
  if (!ts) return "no backup";
  const s = Math.floor(Date.now() / 1000 - ts);
  if (s < 60) return "backup now"; if (s < 3600) return `backup ${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `backup ${Math.floor(s / 3600)}h ago`; return `backup ${Math.floor(s / 86400)}d ago`;
}
function short(sha: string) { return sha ? sha.slice(0, 7) : "—"; }
type Filter = "all" | "production" | "staging" | "preview";
type View = "list" | "grid";

export function BenchesSitesPage({ role }: { role?: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["benches-overview"], queryFn: () => apiGet<BenchesOverview>("/benches/overview"), refetchInterval: 15_000,
  });
  const [filter, setFilter] = useState<Filter>("all");
  const [view, setView] = useState<View>("list");
  const [newSiteBench, setNewSiteBench] = useState<number | null>(null); // -1 = any bench
  const canAdmin = atLeast(role ?? "", "admin");

  const benches = data?.benches ?? [];
  const totalSites = benches.reduce((a, b) => a + b.sites, 0);

  // group matrix rows (sites) by bench id
  const sitesByBench = useMemo(() => {
    const m = new Map<number, MatrixRow[]>();
    for (const r of data?.matrix ?? []) {
      if (filter !== "all" && r.env !== filter) continue;
      if (!m.has(r.bench_id)) m.set(r.bench_id, []);
      m.get(r.bench_id)!.push(r);
    }
    return m;
  }, [data, filter]);

  const visibleBenches = benches.filter((b) => filter === "all" || b.env === filter || (sitesByBench.get(b.id)?.length ?? 0) > 0);

  return (
    <div className="stack">
      <div>
        <div className="rt-eyebrow">FRAPPE · {benches.length} BENCH{benches.length === 1 ? "" : "ES"} · {totalSites} SITE{totalSites === 1 ? "" : "S"}</div>
        <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
          <div className="rt-title" style={{ fontSize: 40 }}>Every bench, every site,<br /><span style={{ color: "var(--brand)" }}>every app version.</span></div>
          <div style={{ display: "flex", gap: 10 }}>
            {canAdmin && <Link className="btn secondary" to="/servers">Connect a bench</Link>}
            {canAdmin && <Button onClick={() => setNewSiteBench(-1)}>+ New site</Button>}
          </div>
        </div>
      </div>

      <div className="grid-2">
        <PanelBox
          title="Benches"
          action={
            <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
              <div className="rtabs">
                {(["all", "production", "staging", "preview"] as Filter[]).map((f) => (
                  <button key={f} className={filter === f ? "on" : ""} onClick={() => setFilter(f)}>
                    {f === "all" ? "All" : f[0].toUpperCase() + f.slice(1)}
                  </button>
                ))}
              </div>
              <div className="view-toggle">
                <button className={view === "list" ? "on" : ""} onClick={() => setView("list")}>List</button>
                <button className={view === "grid" ? "on" : ""} onClick={() => setView("grid")}>Grid</button>
              </div>
            </div>
          }
        >
          {isLoading ? (
            <div style={{ padding: 14 }}><div className="skeleton" style={{ height: 160 }} /></div>
          ) : visibleBenches.length === 0 ? (
            <Empty>No benches. Connect a server, refresh and import its benches.</Empty>
          ) : view === "list" ? (
            <div style={{ padding: 14 }}>
              <div className="bench-list">
                {visibleBenches.map((b) => (
                  <BenchAccordion key={b.id} b={b} sites={sitesByBench.get(b.id) ?? []}
                    canAdmin={canAdmin} onNewSite={() => setNewSiteBench(b.id)} />
                ))}
              </div>
            </div>
          ) : (
            <GridView data={data!} filter={filter} />
          )}
        </PanelBox>

        <div className="stack">
          <JobsPanel benches={benches} />
          <BackupsPanel backups={data?.backups ?? []} />
        </div>
      </div>

      {newSiteBench !== null && (
        <NewSiteModal benchId={newSiteBench > 0 ? newSiteBench : undefined} onClose={() => setNewSiteBench(null)} />
      )}
    </div>
  );
}

function BenchAccordion({ b, sites, canAdmin, onNewSite }: {
  b: BenchCard; sites: MatrixRow[]; canAdmin: boolean; onNewSite: () => void;
}) {
  const nav = useNavigate();
  const healthy = b.server_status === "online";
  const topApps = b.apps.slice(0, 3);

  return (
    <div className="bench-acc">
      <div className="bench-hd" onClick={() => nav(`/benches/${b.id}`)} style={{ cursor: "pointer" }}>
        <div className="bh-main">
          <span className="bh-name"><span className={`bh-sdot ${healthy ? "on" : "off"}`} />{b.name}</span>
          <span className="bh-meta">{b.env ? b.env[0].toUpperCase() + b.env.slice(1) + " · " : ""}{b.server_name} · frappe {b.frappe_version || "—"}</span>
        </div>
        <div className="bh-chips">
          {topApps.map((a) => {
            const behind = a.latest_commit && a.version && a.latest_commit !== a.version;
            return (
              <span key={a.name} className={`apptag ${behind ? "apptag-behind" : ""}`} title={a.branch ? `branch: ${a.branch}` : undefined}>
                {a.name}
                {a.branch && <span className="apptag-branch"> {a.branch}</span>}
                <span className="apptag-cur"> {short(a.version) || "—"}</span>
                {behind && <span className="apptag-latest" title={`latest: ${a.latest_commit}`}> → {short(a.latest_commit)}</span>}
              </span>
            );
          })}
        </div>
        <span className="bh-count">{sites.length} site{sites.length === 1 ? "" : "s"}</span>
        <span className="sr-arrow" style={{ color: "var(--text-faint)", marginLeft: 8 }}>→</span>
      </div>
      {canAdmin && (
        <div className="site-row site-row-add" onClick={(e) => { e.stopPropagation(); onNewSite(); }}>
          <span className="sr-arrow" style={{ color: "var(--brand)" }}>＋</span>
          <span style={{ color: "var(--brand)", fontWeight: 600 }}>New site on {b.name}</span>
        </div>
      )}
    </div>
  );
}

function GridView({ data, filter }: { data: BenchesOverview; filter: Filter }) {
  const nav = useNavigate();
  const rows = data.matrix.filter((r) => filter === "all" || r.env === filter);
  if (rows.length === 0) return <Empty>No sites.</Empty>;
  return (
    <div className="matrix-wrap">
      <table className="matrix">
        <thead>
          <tr><th>Site</th>{data.columns.map((c) => <th key={c}>{c}</th>)}<th>Status</th></tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.site_id} style={{ cursor: "pointer" }} onClick={() => nav(`/sites/${r.site_id}`)}>
              <td className="site-cell">{r.site}<div className="sub">{r.bench}</div></td>
              {data.columns.map((c) => {
                const installed = c in r.versions;
                return <td key={c}>{installed
                  ? <span className={`vtag ${r.env === "staging" || r.env === "preview" ? "new" : ""}`}>{r.versions[c] || "—"}</span>
                  : <span className="vtag none">—</span>}</td>;
              })}
              <td><span className={`badge ${r.status === "active" ? "active" : r.status}`}>{r.status === "active" ? "Live" : r.status}</span></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

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
        <div className="q-foot">{haveData ? "Queue depths from the latest server status." : "Queue depths appear after a server refresh collects them."}</div>
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
            <div className="bk" key={b.site}><span className="bk-site">{b.site}</span><span className="bk-meta">{ago(b.at)}</span></div>
          ))}
      </div>
    </PanelBox>
  );
}
