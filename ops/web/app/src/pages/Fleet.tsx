import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, atLeast, Fleet, FleetServer } from "../api";
import { Button, Empty, PanelBox } from "../ui";

function gb(mb: number) { return mb >= 1024 ? `${Math.round(mb / 1024)} GB` : `${mb} MB`; }
function meterCls(pct: number) { return pct >= 85 ? "hot" : pct >= 70 ? "warn" : ""; }
type Filter = "all" | "bench host" | "runner" | "attention";

export function FleetPage({ role }: { role?: string }) {
  const { data, isLoading } = useQuery({ queryKey: ["fleet"], queryFn: () => apiGet<Fleet>("/servers/fleet"), refetchInterval: 10_000 });
  const [filter, setFilter] = useState<Filter>("all");
  const canAdmin = atLeast(role ?? "", "admin");

  const servers = data?.servers ?? [];
  const counts = useMemo(() => ({
    bench: servers.filter((s) => s.role === "bench host").length,
    runner: servers.filter((s) => s.role === "runner").length,
    attn: (data?.attention ?? []).length,
  }), [servers, data]);
  const visible = servers.filter((s) =>
    filter === "all" ? true :
    filter === "attention" ? !s.online || s.disk_pct >= 85 :
    s.role === filter
  );

  const totalCpu = data?.total_cpu ?? 0;
  const memPct = data && data.total_mem_mb ? Math.round((data.used_mem_mb / data.total_mem_mb) * 100) : 0;

  const enrollCmd = "curl -fsSL https://[YOUR-REDCI-HOST]/agent.sh | sudo sh -s -- \\\n  --token rci_•••••••• --labels bench,prod";

  return (
    <div className="stack">
      <div>
        <div className="rt-eyebrow">FLEET · {data?.total ?? 0} SERVERS · {servers.filter((s) => s.online).length} ONLINE</div>
        <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
          <div className="rt-title" style={{ fontSize: 40 }}>
            The metal behind<br /><span style={{ color: "var(--brand)" }}>every green check.</span>
          </div>
          {canAdmin && (
            <a className="btn" href="/servers" onClick={(e) => { e.preventDefault(); toast.message("Use Servers → Add server to enroll a new agent."); }}>+ Add server</a>
          )}
        </div>
      </div>

      {/* rollup tiles */}
      <div className="fleet-tiles">
        <div className="ftile">
          <div className="ft-top"><span className="ft-label">Agents online</span><span className="ft-aux">{(data?.attention ?? []).filter(a => a.level === "danger").length} offline</span></div>
          <span className="ft-num">{data?.online ?? 0} / {data?.total ?? 0}</span>
          <div className="meter"><i style={{ width: `${data?.total ? (data.online / data.total) * 100 : 0}%` }} /></div>
        </div>
        <div className="ftile">
          <div className="ft-top"><span className="ft-label">Fleet CPU</span><span className="ft-aux">{totalCpu} vCPU</span></div>
          <span className="ft-num">{totalCpu}</span>
          <div className="meter"><i style={{ width: "100%" }} /></div>
        </div>
        <div className="ftile">
          <div className="ft-top"><span className="ft-label">Memory used</span><span className="ft-aux">of {gb(data?.total_mem_mb ?? 0)}</span></div>
          <span className="ft-num">{memPct}%</span>
          <div className="meter"><i className={meterCls(memPct)} style={{ width: `${memPct}%` }} /></div>
        </div>
        <div className="ftile">
          <div className="ft-top"><span className="ft-label">Running jobs</span></div>
          <span className="ft-num">{data?.running_jobs ?? 0}</span>
          <div className="meter"><i style={{ width: `${Math.min(100, (data?.running_jobs ?? 0) * 20)}%`, background: "var(--info)" }} /></div>
        </div>
      </div>

      {/* servers + side */}
      <div className="grid-2">
        <PanelBox
          title="Servers"
          action={
            <div className="rtabs">
              <button className={filter === "all" ? "on" : ""} onClick={() => setFilter("all")}>All <span className="cnt">{servers.length}</span></button>
              <button className={filter === "bench host" ? "on" : ""} onClick={() => setFilter("bench host")}>Bench hosts <span className="cnt">{counts.bench}</span></button>
              <button className={filter === "runner" ? "on" : ""} onClick={() => setFilter("runner")}>Runners <span className="cnt">{counts.runner}</span></button>
              <button className={filter === "attention" ? "on" : ""} onClick={() => setFilter("attention")}>Attention <span className="cnt">{counts.attn}</span></button>
            </div>
          }
        >
          <div style={{ padding: 14 }}>
            {isLoading ? <div className="skeleton" style={{ height: 160 }} /> :
              visible.length === 0 ? <Empty>No servers.</Empty> : (
                <div className="srv-grid">
                  {visible.map((s) => <ServerCard key={s.id} s={s} />)}
                </div>
              )}
          </div>
        </PanelBox>

        <div className="stack">
          <AttentionPanel items={data?.attention ?? []} />
          <AgentVersions data={data} />
          <PanelBox title="Install an agent">
            <div className="install-box">
              <p>Run this on any Linux server. It registers itself, detects benches, and starts picking up jobs.</p>
              <div className="cmd">{enrollCmd}</div>
              <Button variant="primary" style={{ width: "100%", marginTop: 12, justifyContent: "center" }}
                onClick={() => { navigator.clipboard?.writeText(enrollCmd); toast.success("Command copied"); }}>
                Copy command
              </Button>
            </div>
          </PanelBox>
        </div>
      </div>
    </div>
  );
}

function ServerCard({ s }: { s: FleetServer }) {
  const dotCls = !s.online ? "off" : s.disk_pct >= 85 || s.mem_pct >= 85 ? "warn" : "on";
  const cardCls = !s.online ? "off" : (s.disk_pct >= 85 || s.mem_pct >= 85) ? "attn" : "";
  const roleCls = s.role === "bench host" ? "bench" : s.role === "runner" ? "runner" : "database";
  const metric = (k: string, pct: number) => (
    <div className="srv-metric">
      <span className="sm-k">{k}</span>
      <span className="sm-bar"><i className={meterCls(pct)} style={{ width: `${s.online ? pct : 0}%` }} /></span>
      <span className="sm-v">{s.online ? `${pct}%` : "—"}</span>
    </div>
  );
  return (
    <div className={`srv-card ${cardCls}`}>
      <div className="srv-top">
        <div>
          <div className="srv-name"><span className={`sdot ${dotCls}`} />{s.name}</div>
          <div className="srv-spec">{s.cpus || "?"} vCPU · {gb(s.mem_total_mb)} · {s.os || "linux"}</div>
        </div>
        <span className={`role-tag ${roleCls}`}>{s.role}</span>
      </div>
      <div className="srv-metrics">
        {metric("CPU", s.cpu_pct)}
        {metric("Memory", s.mem_pct)}
        {metric("Disk", s.disk_pct)}
      </div>
      <div className="srv-foot">
        <span>agent {s.agent_version || "—"}{s.benches ? ` · ${s.benches} bench${s.benches === 1 ? "" : "es"}` : ""}</span>
        <span>{s.online ? `up ${s.uptime_days}d` : "offline"}</span>
      </div>
    </div>
  );
}

function AttentionPanel({ items }: { items: Fleet["attention"] }) {
  return (
    <PanelBox title="Needs attention" action={items.length > 0 ? <span className="badge failed">{items.length}</span> : null}>
      {items.length === 0 ? <div className="empty">All clear.</div> : (
        <div className="attn-list">
          {items.map((a, i) => (
            <div key={i} className={`attn-item ${a.level}`}>
              <span className="ai-ic">{a.level === "danger" ? "✕" : a.level === "warn" ? "!" : "↑"}</span>
              <div>
                <div className="ai-title">{a.title}</div>
                <div className="ai-detail">{a.detail}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </PanelBox>
  );
}

function AgentVersions({ data }: { data?: Fleet }) {
  const vers = data?.agent_versions ?? [];
  const total = vers.reduce((a, v) => a + v.count, 0) || 1;
  const cls = (v: { latest: boolean; version: string }) => v.latest ? "latest" : v.version === data?.latest_agent ? "mid" : "old";
  return (
    <PanelBox title="Agent versions">
      <div className="verbar">
        {vers.map((v) => <i key={v.version} className={cls(v)} style={{ width: `${(v.count / total) * 100}%` }} />)}
      </div>
      <div className="verlist">
        {vers.length === 0 ? <span className="muted" style={{ fontSize: 13 }}>No agents reporting.</span> :
          vers.map((v) => (
            <div className="vrow" key={v.version}>
              <span className={`vdot ${cls(v)}`} />
              <span className="vname">{v.version}</span>
              {v.latest && <span className="badge healthy" style={{ padding: "1px 7px" }}>latest</span>}
              <span className="vcount">{v.count} agent{v.count === 1 ? "" : "s"}</span>
            </div>
          ))}
      </div>
    </PanelBox>
  );
}
