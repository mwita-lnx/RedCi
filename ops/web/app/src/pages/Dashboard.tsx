import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Audit, Dashboard, Deploy, Me, Pipeline } from "../api";
import { PanelBox } from "../ui";

function startOfTodayUnix() { const d = new Date(); d.setHours(0, 0, 0, 0); return Math.floor(d.getTime() / 1000); }
function ago(ts: number): string {
  const s = Math.floor(Date.now() / 1000 - ts);
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}
function initials(name: string) {
  const p = name.replace(/@.*/, "").split(/[.\s_-]+/).filter(Boolean);
  return ((p[0]?.[0] ?? "?") + (p[1]?.[0] ?? "")).toUpperCase();
}
const RUNNING = ["running", "queued", "ready", "claimed"];

// Map a deploy status to a stage-bar colour class, and render N stage bars.
function stageBars(status: string) {
  const cls =
    status === "succeeded" ? ["ok", "ok", "ok", "ok", "ok"] :
    status === "failed" ? ["ok", "ok", "fail", "pend", "pend"] :
    status === "running" ? ["ok", "run", "pend", "pend", "pend"] :
    status === "cancelled" ? ["ok", "pend", "pend", "pend", "pend"] :
    ["pend", "pend", "pend", "pend", "pend"];
  return cls;
}

type Filter = "all" | "running" | "failed";

export function DashboardPage() {
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: () => apiGet<Me>("/me") });
  const { data: d } = useQuery({ queryKey: ["dashboard"], queryFn: () => apiGet<Dashboard>("/dashboard"), refetchInterval: 10_000 });
  const { data: deploys } = useQuery({
    queryKey: ["deploys"], queryFn: () => apiGet<Deploy[]>("/deploys"),
    refetchInterval: (q) => (q.state.data as Deploy[] | undefined)?.some((x) => RUNNING.includes(x.status)) ? 3000 : 10_000,
  });
  const { data: pipelines } = useQuery({ queryKey: ["pipelines"], queryFn: () => apiGet<Pipeline[]>("/pipelines") });
  const { data: audit } = useQuery({ queryKey: ["audit"], queryFn: () => apiGet<Audit[]>("/audit") });
  const [filter, setFilter] = useState<Filter>("all");

  const rows = deploys ?? [];
  const running = rows.filter((r) => r.status === "running");
  const failed = rows.filter((r) => r.status === "failed");

  const stats = useMemo(() => {
    const today0 = startOfTodayUnix();
    const today = rows.filter((r) => r.created_at >= today0);
    const finished = rows.filter((r) => r.status === "succeeded" || r.status === "failed");
    const passed = finished.filter((r) => r.status === "succeeded").length;
    const passRate = finished.length ? ((passed / finished.length) * 100).toFixed(1) : "—";
    const prodDeploys = rows.filter((r) => r.kind === "pipeline_promote" && r.status === "succeeded").length;
    return { runsToday: today.length, passRate, running: running.length, failed: failed.length, prodDeploys };
  }, [rows, running.length, failed.length]);

  const firstFail = failed[0];
  const shipping = stats.failed === 0;

  const visible = rows.filter((r) =>
    filter === "running" ? r.status === "running" :
    filter === "failed" ? r.status === "failed" : true
  ).slice(0, 8);

  // sparkline: last 7 finished runs, newest last
  const spark = rows.slice(0, 7).reverse();
  const sparkCls = (s: string) => s === "failed" ? "hot" : s === "running" ? "b" : s === "succeeded" ? "" : "";

  const dateLabel = new Date().toLocaleDateString(undefined, { weekday: "short", day: "2-digit", month: "short" }).toUpperCase();

  return (
    <div className="stack">
      {/* ---- top bar ---- */}
      <div className="topbar">
        <div className="search">
          <span className="mag">⌕</span>
          <input type="text" placeholder="Search pipelines, commits, runs…" />
          <span className="kbd">⌘K</span>
        </div>
        <button className="icon-btn" title="Notifications">◔</button>
        <Link className="btn" to="/pipelines">+ New pipeline</Link>
        <span className="avatar-sm">{initials(me?.email ?? "R")}</span>
      </div>

      {/* ---- hero ---- */}
      <div className="hero">
        <div className="hero-main">
          <div className="eyebrow">{dateLabel} · {shipping ? "ALL SYSTEMS SHIPPING" : "ATTENTION NEEDED"}</div>
          <h1>
            {stats.runsToday} run{stats.runsToday === 1 ? "" : "s"} today.
            {shipping
              ? <span className="accent" style={{ color: "var(--ok)" }}>all green.</span>
              : <span className="accent">{stats.failed} need{stats.failed === 1 ? "s" : ""} you.</span>}
          </h1>
          <p className="hero-sub">
            {firstFail
              ? <>
                  <b>{firstFail.kind} #{firstFail.id}</b> failed {ago(firstFail.created_at)} — it is
                  blocking downstream deploys. Jump in to unblock the train.
                </>
              : "Everything is passing across your fleet. Nothing needs your attention right now."}
          </p>
          <div className="hero-actions">
            {firstFail
              ? <Link className="btn" to={firstFail.last_job_id ? `/jobs/${firstFail.last_job_id}` : "/deploys"}>Open failure →</Link>
              : <Link className="btn" to="/deploys">View runs →</Link>}
            <Link className="btn secondary" to="/deploys">↻ Rerun failed jobs</Link>
          </div>
        </div>
        <div className="terminal">
          <div className="t-cmd">$ redci status --live</div>
          {running.slice(0, 3).map((r) => (
            <div key={r.id} className="t-row">
              <span className="tdot run" /><b>{r.kind}</b> #{r.id}<span className="tmeta">{r.target_type}:{r.target_id}</span>
            </div>
          ))}
          {failed.slice(0, 2).map((r) => (
            <div key={r.id} className="t-row fail">
              ✕ <b>{r.kind}</b> #{r.id}<span className="tmeta">{r.target_type}:{r.target_id}</span>
            </div>
          ))}
          <div className="t-row pass">✓ {rows.filter((r) => r.status === "succeeded").length} others passed</div>
        </div>
      </div>

      {/* ---- stat tiles ---- */}
      <div className="cards">
        <Tile label="Runs today" delta={`${stats.runsToday}`} deltaCls="flat" num={`${stats.runsToday}`} spark={spark} sparkCls={sparkCls} />
        <Tile label="Success rate" delta="" deltaCls="up" num={stats.passRate === "—" ? "—" : `${stats.passRate}%`} spark={spark} sparkCls={() => "g"} highlightLast="g" />
        <Tile label="Running now" delta="" deltaCls="flat" num={`${stats.running}`} spark={spark} sparkCls={() => "b"} highlightLast="b" />
        <Tile label="Prod deploys" delta="total" deltaCls="flat" num={`${stats.prodDeploys}`} spark={spark} sparkCls={() => ""} highlightLast="y" />
      </div>

      {/* ---- recent runs + side ---- */}
      <div className="grid-2">
        <PanelBox
          title="Recent runs"
          action={
            <div className="rtabs">
              <button className={filter === "all" ? "on" : ""} onClick={() => setFilter("all")}>All <span className="cnt">{rows.length}</span></button>
              <button className={filter === "running" ? "on" : ""} onClick={() => setFilter("running")}>Running <span className="cnt">{running.length}</span></button>
              <button className={filter === "failed" ? "on" : ""} onClick={() => setFilter("failed")}>Failed <span className="cnt">{failed.length}</span></button>
            </div>
          }
        >
          {visible.length === 0 ? <div className="empty">No runs.</div> : (
            <table>
              <thead><tr><th>Status</th><th>Pipeline · Commit</th><th>Target</th><th>Stages</th><th>Time</th><th>When</th></tr></thead>
              <tbody>
                {visible.map((r) => (
                  <tr key={r.id}>
                    <td><span className={`badge ${r.status}`}>{r.status}</span></td>
                    <td>
                      <div className="run-primary">{r.kind} <span className="num">#{r.id}</span></div>
                      {r.error ? <div className="run-sub" style={{ color: "var(--danger)" }}>{r.error}</div> : <div className="run-sub">{r.trigger} trigger</div>}
                    </td>
                    <td className="branch">{r.target_type}:{r.target_id}</td>
                    <td><div className="stagebars">{stageBars(r.status).map((c, i) => <i key={i} className={c} />)}</div></td>
                    <td className="cell-time">{r.last_job_id ? `#${r.last_job_id}` : "—"}</td>
                    <td className="muted">{ago(r.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <div style={{ display: "flex", justifyContent: "space-between", padding: "12px 18px", fontSize: 12.5, color: "var(--text-muted)" }}>
            <span>Showing latest runs across {d?.sites ?? 0} sites</span>
            <Link to="/deploys" className="link-head">View all runs →</Link>
          </div>
        </PanelBox>

        <div className="stack">
          <PanelBox title="Environments" action={<Link className="link-head" to="/pipelines">All →</Link>}>
            {!pipelines?.length ? <div className="empty">No pipelines yet.</div> : (
              <div style={{ padding: "12px 16px", display: "flex", flexDirection: "column", gap: 10 }}>
                {pipelines.slice(0, 3).map((p) => {
                  const prod = p.envs[p.envs.length - 1];
                  const stg = p.envs.length > 1 ? p.envs[p.envs.length - 2] : undefined;
                  return (
                    <div key={p.id} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                      {prod && <EnvRow name={`${p.name} · ${prod.name}`} ver={prod.current_commit} status={prod.status} />}
                      {stg && <EnvRow name={`${p.name} · ${stg.name}`} ver={stg.current_commit} status={stg.status} focus />}
                    </div>
                  );
                })}
              </div>
            )}
          </PanelBox>

          <PanelBox title="Flaky tests" action={<span className="muted" style={{ fontSize: 12 }}>no data</span>}>
            <div className="empty" style={{ padding: "20px 16px" }}>No test telemetry wired yet.</div>
          </PanelBox>

          <PanelBox title="Activity">
            {!audit?.length ? <div className="empty">No activity.</div> : (
              <div className="activity">
                {audit.slice(0, 6).map((a) => (
                  <div key={a.id} className="ac">
                    <span className="ac-av">{initials(a.actor || "system")}</span>
                    <div>
                      <div className="ac-body"><b>{a.actor || "system"}</b> {a.action.replace(/[._]/g, " ")} {a.object_type}{a.object_id ? ` #${a.object_id}` : ""}</div>
                      <div className="ac-time">{ago(a.created_at)}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </PanelBox>
        </div>
      </div>
    </div>
  );
}

function Tile({ label, delta, deltaCls, num, spark, sparkCls, highlightLast }: {
  label: string; delta: string; deltaCls: string; num: string;
  spark: Deploy[]; sparkCls: (s: string) => string; highlightLast?: string;
}) {
  return (
    <div className="tile">
      <div className="tile-top">
        <span className="tile-label">{label}</span>
        {delta && <span className={`tile-delta ${deltaCls}`}>{delta}</span>}
      </div>
      <span className="tile-num">{num}</span>
      <div className="spark">
        {(spark.length ? spark : Array.from({ length: 7 }).map(() => ({ status: "" } as Deploy))).map((s, i, arr) => {
          const last = i === arr.length - 1;
          const cls = last && highlightLast ? highlightLast : sparkCls(s.status);
          return <span key={i} className={cls} style={{ height: `${30 + ((i * 37) % 60)}%` }} />;
        })}
      </div>
    </div>
  );
}

function EnvRow({ name, ver, status, focus }: { name: string; ver: string; status: string; focus?: boolean }) {
  const deploying = status === "deploying";
  return (
    <div className={`env-row ${focus ? "focus" : ""}`}>
      <div className="er-top">
        <span className="er-name">{name}</span>
        <span className={`er-health ${deploying ? "deploy" : "ok"}`}>
          <span className="d" />{deploying ? "Deploying" : status === "healthy" ? "Healthy" : status}
        </span>
      </div>
      <div className="er-ver">{ver ? ver.slice(0, 7) : "—"} <span className="sha">· {ver ? ver.slice(7, 13) : ""}</span></div>
      {deploying && <div className="progress"><i style={{ width: "64%" }} /></div>}
    </div>
  );
}
