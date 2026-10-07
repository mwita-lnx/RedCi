import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Job } from "../api";
import { Badge, PanelBox } from "../ui";

interface Line { seq: number; stream: string; line: string; ts?: number }
type StStatus = "succeeded" | "failed" | "running" | "pending";
interface Step { name: string; status: StStatus; dur?: number }

function fmtDur(s?: number) {
  if (s == null) return "";
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60); return `${m}m ${String(s % 60).padStart(2, "0")}s`;
}

// Parse "== step: <name>" markers (+ step-failure lines) into ordered steps
// with durations computed from log timestamps.
function deriveSteps(lines: Line[], jobStatus: string): Step[] {
  const marks: { name: string; ts?: number }[] = [];
  const failed = new Set<string>();
  for (const l of lines) {
    const m = l.line.match(/^==\s*step:\s*(.+?)\s*$/);
    if (m) marks.push({ name: m[1], ts: l.ts });
    const f = l.line.match(/step "(.+?)" failed/);
    if (f) failed.add(f[1]);
  }
  const lastTs = lines.length ? lines[lines.length - 1].ts : undefined;
  return marks.map((mk, i) => {
    const next = marks[i + 1]?.ts ?? lastTs;
    const dur = mk.ts != null && next != null ? Math.max(0, next - mk.ts) : undefined;
    const isLast = i === marks.length - 1;
    let status: StStatus = "succeeded";
    if (failed.has(mk.name)) status = "failed";
    else if (isLast && (jobStatus === "running" || jobStatus === "claimed")) status = "running";
    else if (isLast && jobStatus === "failed") status = "failed";
    return { name: mk.name, status, dur };
  });
}

// Group steps into DAG columns by phase keyword; unmatched steps fall into a
// trailing "Run" column so every step is shown.
const PHASES: { key: string; match: RegExp }[] = [
  { key: "Setup", match: /checkout|clone|fetch|restore|cache|get[-\s]?app|record|backup/i },
  { key: "Build", match: /build|pip|yarn|install|docker|image|compile/i },
  { key: "Test", match: /test|lint|migrate|check/i },
  { key: "Deploy", match: /deploy|nginx|setup nginx|restart|site|restore|maintenance/i },
  { key: "Release", match: /cert|certificate|ssl|release|approval|promote/i },
];
function groupStages(steps: Step[]) {
  const cols = PHASES.map((p) => ({ key: p.key, steps: [] as Step[] }));
  const extra: Step[] = [];
  for (const s of steps) {
    const p = PHASES.find((ph) => ph.match.test(s.name));
    if (p) cols.find((c) => c.key === p.key)!.steps.push(s);
    else extra.push(s);
  }
  if (extra.length) cols.push({ key: "Run", steps: extra });
  return cols.filter((c) => c.steps.length);
}
function colStatus(steps: Step[]): StStatus {
  if (steps.some((s) => s.status === "failed")) return "failed";
  if (steps.some((s) => s.status === "running")) return "running";
  if (steps.every((s) => s.status === "succeeded")) return "succeeded";
  return "pending";
}

export function JobPage() {
  const { id } = useParams();
  const { data: job } = useQuery({ queryKey: ["job", id], queryFn: () => apiGet<Job>(`/jobs/${id}`) });
  const [lines, setLines] = useState<Line[]>([]);
  const [status, setStatus] = useState<string>("");
  const boxRef = useRef<HTMLDivElement>(null);
  const seeded = useRef(false);

  useEffect(() => {
    if (job && !seeded.current) {
      seeded.current = true;
      setLines(job.logs.map((l) => ({ seq: l.seq, stream: l.stream, line: l.line, ts: l.ts })));
      setStatus(job.status);
    }
  }, [job]);

  useEffect(() => {
    if (!job || ["succeeded", "failed", "lost", "cancelled"].includes(job.status)) return;
    const es = new EventSource(`/jobs/${id}/stream`);
    const parse = (html: string) => {
      const div = document.createElement("div");
      div.innerHTML = html;
      const el = div.firstElementChild as HTMLElement | null;
      return { stream: (el?.className || "").replace("logline", "").trim(), line: el?.textContent || "" };
    };
    es.addEventListener("log", (e: MessageEvent) => {
      const seq = Number((e as MessageEvent & { lastEventId: string }).lastEventId || 0);
      const { stream, line } = parse(e.data);
      setLines((prev) => prev.some((l) => l.seq === seq) ? prev : [...prev, { seq, stream, line, ts: Math.floor(Date.now() / 1000) }]);
    });
    es.addEventListener("status", (e: MessageEvent) => {
      const div = document.createElement("div"); div.innerHTML = e.data;
      setStatus(div.textContent?.trim() || "");
    });
    es.addEventListener("done", () => es.close());
    es.onerror = () => es.close();
    return () => es.close();
  }, [job, id]);

  useEffect(() => { boxRef.current?.scrollTo(0, boxRef.current.scrollHeight); }, [lines]);

  const steps = useMemo(() => deriveSteps(lines, status), [lines, status]);
  const cols = useMemo(() => groupStages(steps), [steps]);
  const total = job?.started && job?.finished ? job.finished - job.started : undefined;

  const title = job?.commit ? `${job.kind} ${job.commit.slice(0, 7)}` : (job?.kind ?? job?.type ?? `Run #${id}`);
  const runnerLabel = "runner local";

  return (
    <>
      <div className="breadcrumb">
        <Link to="/">redci</Link><span className="sep">/</span>
        <Link to="/deploys">runs</Link><span className="sep">/</span>
        <span className="cur">#{id}</span>
      </div>

      <div className="run-head">
        <div>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Badge status={status || job?.status || ""} />
            <span className="muted" style={{ fontFamily: "var(--font-mono)", fontSize: 13 }}>{job?.type} · run #{id}</span>
          </div>
          <div className="run-title">{title}</div>
          <div className="run-metaline">
            {job?.commit && <span className="mi"><span className="muted">⎇</span><span className="mono">{job.commit.slice(0, 7)}</span></span>}
            {job?.trigger && <span className="mi"><span className="muted">via</span> <b>{job.trigger}</b></span>}
            {job?.target_type && <span className="mi mono">{job.target_type}:{job.target_id}</span>}
            {job?.created_at && <span className="mi">Started {new Date(job.created_at * 1000).toLocaleTimeString()}</span>}
            {total != null && <span className="mi">Total <b>{fmtDur(total)}</b></span>}
          </div>
        </div>
        <div className="run-head-actions">
          <Link className="btn secondary" to="/deploys">Rerun all</Link>
          {status === "failed" && <button className="btn" onClick={() => alert("Re-run is triggered from the originating action (webhook/UI).")}>↻ Rerun failed job</button>}
        </div>
      </div>

      {cols.length > 0 && (
        <PanelBox>
          <div className="dag">
            {cols.map((c) => {
              const cs = colStatus(c.steps);
              return (
                <div className="dag-col" key={c.key}>
                  <div className="col-head">{c.key}<span className={`cdot ${cs === "succeeded" ? "ok" : cs}`} /></div>
                  {c.steps.map((s) => (
                    <div className={`node ${s.status}`} key={s.name}>
                      <span className="nic">{s.status === "succeeded" ? "✓" : s.status === "failed" ? "✕" : s.status === "running" ? "•" : "–"}</span>
                      <span className="nbody">
                        <span className="nname">{s.name}</span>
                        {s.dur != null && <span className="ndur">{fmtDur(s.dur)}</span>}
                      </span>
                    </div>
                  ))}
                </div>
              );
            })}
          </div>
        </PanelBox>
      )}

      <div className="grid-2">
        {/* log viewer */}
        <div className="logview">
          <div className="lv-head">
            <div className="lv-title">
              {status === "failed" ? <span className="x">✕</span> : null}
              <b>{steps.find((s) => s.status === "failed")?.name ?? "Logs"}</b>
              <span className="lv-meta">{total != null ? fmtDur(total) : ""} · {runnerLabel}</span>
            </div>
          </div>
          <div className="logrows" ref={boxRef}>
            {lines.length === 0 ? <div className="lr"><span className="ln">·</span><span className="lt" style={{ color: "var(--ok)" }}>waiting for output…</span></div> :
              lines.map((l, i) => (
                <div key={l.seq} className={`lr ${l.stream}`}>
                  <span className="ln">{i + 1}</span>
                  <span className="lt">{l.line}</span>
                </div>
              ))}
          </div>
        </div>

        {/* side panels */}
        <div className="stack">
          <PanelBox>
            <div className="why">
              <div className="why-tag">✦ why it failed</div>
              {job?.error ? (
                <>
                  <h3>The run failed during “{steps.find((s) => s.status === "failed")?.name ?? "a step"}”.</h3>
                  <p>{job.error}</p>
                </>
              ) : (
                <>
                  <h3>{status === "succeeded" ? "This run completed successfully." : "Run in progress…"}</h3>
                  <p className="muted">No failure recorded.</p>
                </>
              )}
            </div>
          </PanelBox>

          <PanelBox title="Tests">
            <div className="muted" style={{ padding: "8px 18px 16px", fontSize: 13 }}>
              No test telemetry wired yet. Steps: {steps.length} · failed: {steps.filter((s) => s.status === "failed").length}.
            </div>
          </PanelBox>

          <PanelBox title="Artifacts">
            <div className="muted" style={{ padding: "8px 18px 16px", fontSize: 13 }}>
              No artifact store configured.
            </div>
          </PanelBox>
        </div>
      </div>

      <Link to="/deploys" className="muted" style={{ fontSize: 13 }}>← Back to runs</Link>
    </>
  );
}
