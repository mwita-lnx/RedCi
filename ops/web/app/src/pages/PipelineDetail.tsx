import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiDelete, apiGet, apiPost, atLeast, AppSource, Deploy, GhCommit, GithubStatus, Pipeline, PipelineEnv } from "../api";
import { Button, Modal, PanelBox } from "../ui";

function shortSha(c: string) { return c ? c.slice(0, 7) : "—"; }
function ago(ts: number) {
  if (!ts) return "—";
  const s = Math.floor(Date.now() / 1000 - ts);
  if (s < 60) return "just now"; if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`; return `${Math.floor(s / 86400)}d ago`;
}
// Role label by position: first=Preview, last=Production, middle=Staging.
function roleOf(i: number, n: number): "preview" | "staging" | "production" {
  if (i === n - 1) return "production";
  if (i === 0 && n > 2) return "preview";
  return "staging";
}

export function PipelineDetailPage({ role }: { role?: string }) {
  const { id } = useParams();
  const qc = useQueryClient();
  const nav = useNavigate();
  const { data: p, isLoading } = useQuery({
    queryKey: ["pipeline", id], queryFn: () => apiGet<Pipeline>(`/pipelines/${id}`),
    refetchInterval: (q) => (q.state.data as Pipeline | undefined)?.envs.some((e) => e.status === "deploying") ? 2500 : 8000,
  });
  const { data: deploys } = useQuery({ queryKey: ["deploys"], queryFn: () => apiGet<Deploy[]>("/deploys") });
  const [promoteEnv, setPromoteEnv] = useState<PipelineEnv | null>(null);
  const canDeploy = atLeast(role ?? "", "deployer");
  const canAdmin = atLeast(role ?? "", "admin");

  const del = useMutation({
    mutationFn: () => apiDelete(`/pipelines/${id}`),
    onSuccess: () => { toast.success("Pipeline deleted"); qc.invalidateQueries({ queryKey: ["pipelines"] }); nav("/pipelines"); },
    onError: (e: Error) => toast.error(e.message),
  });

  // deploys-per-day for the last 14 days (from pipeline_promote runs on this app).
  const perDay = useMemo(() => {
    const days: { label: string; count: number }[] = [];
    const now = new Date(); now.setHours(0, 0, 0, 0);
    const promotes = (deploys ?? []).filter((d) => d.kind === "pipeline_promote");
    for (let k = 13; k >= 0; k--) {
      const day = new Date(now); day.setDate(now.getDate() - k);
      const start = Math.floor(day.getTime() / 1000), end = start + 86400;
      const count = promotes.filter((d) => d.created_at >= start && d.created_at < end).length;
      days.push({ label: day.toLocaleDateString(undefined, { day: "numeric", month: "short" }), count });
    }
    return days;
  }, [deploys]);
  const maxDay = Math.max(1, ...perDay.map((d) => d.count));

  if (isLoading || !p) return <div className="rt-title">Loading…</div>;

  const n = p.envs.length;
  const awaitingIdx = p.envs.findIndex((e, i) => i > 0 && p.envs[i - 1].current_commit && e.current_commit !== p.envs[i - 1].current_commit && e.require_approval);
  const awaiting = awaitingIdx >= 0 ? p.envs[awaitingIdx] : undefined;
  const pending = awaiting ? p.envs[awaitingIdx - 1].current_commit : "";

  return (
    <div className="stack">
      <div>
        <div className="rt-eyebrow">RELEASE TRAIN · {p.name.toUpperCase()}</div>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
          <div className="rt-title">From commit to customers.</div>
          <div style={{ display: "flex", gap: 10 }}>
            <span className="app-select">{p.app} ▾</span>
            {canAdmin && <Button variant="danger" onClick={() => { if (confirm("Delete this pipeline? Sites are not affected.")) del.mutate(); }}>Delete</Button>}
          </div>
        </div>
      </div>

      {awaiting && (
        <div className="approve-banner">
          <div className="ab-left">
            <span className="clock">◷</span>
            <span><b>{shortSha(pending)}</b> is waiting for {awaiting.name} approval — needs a reviewer to ship.</span>
          </div>
          {canDeploy && (
            <div className="ab-actions">
              <Button variant="secondary" onClick={() => setPromoteEnv(awaiting)}>Review</Button>
              <Button onClick={() => setPromoteEnv(awaiting)}>Approve &amp; ship</Button>
            </div>
          )}
        </div>
      )}

      {/* release train */}
      <div className="train">
        {p.envs.map((e, i) => (
          <span key={e.id} style={{ display: "contents" }}>
            <EnvCard env={e} roleLabel={roleOf(i, n)} canDeploy={canDeploy}
              prevCommit={i > 0 ? p.envs[i - 1].current_commit : ""}
              onPromote={() => setPromoteEnv(e)} />
            {i < n - 1 && <span className="train-arrow">→</span>}
          </span>
        ))}
      </div>

      {/* change timeline — each release's journey across the nodes */}
      <PanelBox title="Change timeline" action={<span className="muted" style={{ fontSize: 12 }}>each release's journey through the train</span>}>
        {!p.timeline?.length ? (
          <div className="empty">No promotions yet. Deploy a commit into the first environment to start tracking its journey.</div>
        ) : (
          <div className="timeline">
            {p.timeline.map((c) => (
              <div className="tl-change" key={c.commit}>
                <div className="tl-commit">
                  <span className="sha">{c.commit.slice(0, 7)}</span>
                  <span className="when">updated {ago(c.updated_at)}</span>
                </div>
                <div className="tl-hops">
                  {c.hops.map((h, i) => (
                    <div className="tl-hop" key={h.env}>
                      <div className={`tl-node ${h.reached ? (h.status === "failed" ? "failed" : h.status === "running" || h.status === "queued" ? "running" : "reached") : ""}`}>
                        <span className="tn-dot" />
                        <span className="tn-env">{h.env}</span>
                        {h.reached
                          ? (h.run ? <Link className="tn-meta" to={`/jobs/${h.run}`}>run #{h.run}</Link> : <span className="tn-meta">{h.status}</span>)
                          : <span className="tn-meta">—</span>}
                      </div>
                      {i < c.hops.length - 1 && <span className={`tl-conn ${c.hops[i + 1].reached ? "done" : ""}`} />}
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </PanelBox>

      <div className="grid-2">
        {/* deploys per day */}
        <PanelBox title="Deploys per day" action={<span className="muted" style={{ fontSize: 12 }}>last 14 days · promotions</span>}>
          <div style={{ padding: "4px 18px 16px" }}>
            <div className="dpd">
              {perDay.map((d, i) => (
                <div key={i} className={`d-bar ${i === perDay.length - 1 ? "today" : ""}`}>
                  <span className="d-n">{d.count || ""}</span>
                  <span className="d-fill" style={{ height: `${(d.count / maxDay) * 100}%` }} />
                </div>
              ))}
            </div>
            <div className="dpd-axis"><span>{perDay[0]?.label}</span><span>Today</span></div>
            <div className="dpd-stats">
              <div><div className="k">Lead time</div><div className="v">—</div></div>
              <div><div className="k">Change fail rate</div><div className="v">—</div></div>
              <div><div className="k">MTTR</div><div className="v">—</div></div>
            </div>
          </div>
        </PanelBox>

        {/* history */}
        <PanelBox title="History">
          {p.envs.every((e) => !e.current_commit) ? <div className="empty">No promotions yet.</div> : (
            <table>
              <tbody>
                {[...p.envs].reverse().filter((e) => e.current_commit).map((e) => (
                  <tr className="hist-row" key={e.id}>
                    <td className="hist-ver">{shortSha(e.current_commit)}</td>
                    <td><span className={`badge ${e.status}`}>{e.name}</span></td>
                    <td className="muted">{e.site_domain}</td>
                    <td className="muted">{e.server_name}</td>
                    <td className="muted" style={{ textAlign: "right" }}>{e.agent_online ? "online" : "offline"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </PanelBox>
      </div>

      <Link to="/pipelines" className="muted" style={{ fontSize: 13 }}>← Back to pipelines</Link>

      {promoteEnv && <PromoteModal pipelineId={id!} env={promoteEnv} pipeline={p} onClose={() => setPromoteEnv(null)} />}
    </div>
  );
}

function EnvCard({ env, roleLabel, canDeploy, prevCommit, onPromote }: {
  env: PipelineEnv; roleLabel: "preview" | "staging" | "production"; canDeploy: boolean; prevCommit: string; onPromote: () => void;
}) {
  const deploying = env.status === "deploying";
  const healthy = env.status === "healthy";
  const healthCls = deploying ? "deploy" : healthy ? "ok" : "idle";
  const healthTxt = deploying ? "Deploying" : healthy ? "Healthy" : env.status;
  const title = roleLabel === "preview" ? "Preview" : roleLabel === "staging" ? "Staging" : "Production";

  return (
    <div className={`env-card ${roleLabel}`}>
      <div className="ec-top">
        <span className="ec-name">{title}</span>
        <span className={`ec-health ${healthCls}`}><span className="d" />{healthTxt}</span>
      </div>

      {roleLabel === "preview" ? (
        <>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div className="ec-branch"><span>{env.site_domain}</span><span className={`st ${env.current_commit ? "up" : "building"}`}><span className="d" />{env.current_commit ? "up" : "building"}</span></div>
          </div>
          <div className="ec-note">Env on {env.server_name} · {env.agent_online ? "agent online" : "agent offline"}</div>
        </>
      ) : (
        <>
          <div className="ec-ver">{shortSha(env.current_commit)}</div>
          <div className="ec-sub">{env.site_domain} · {env.server_name}</div>
          {deploying && <div className="ec-progress"><i style={{ width: "64%" }} /></div>}
          {roleLabel === "staging" ? (
            <div className="ec-metrics">
              <div className="ec-metric"><div className="m-k">site</div><div className="m-v">{env.site_status}</div></div>
              <div className="ec-metric"><div className="m-k">agent</div><div className="m-v">{env.agent_online ? "online" : "offline"}</div></div>
              <div className="ec-metric"><div className="m-k">approval</div><div className="m-v">{env.require_approval ? "yes" : "auto"}</div></div>
            </div>
          ) : (
            <>
              <div className="ec-regions">
                <span className="ec-region">{env.site_domain}</span>
                <span className="ec-region">{env.agent_online ? "agent ✓" : "agent ✕"}</span>
              </div>
              {canDeploy && <button className="btn rollback" onClick={onPromote} style={{ width: "100%", justifyContent: "center" }}>Deploy / roll back</button>}
            </>
          )}
          {roleLabel === "staging" && canDeploy && (
            <button className="btn secondary" onClick={onPromote} style={{ justifyContent: "center" }}>
              Deploy to {env.name}{prevCommit && env.current_commit !== prevCommit ? " (behind)" : ""}
            </button>
          )}
        </>
      )}
    </div>
  );
}

function PromoteModal({ pipelineId, env, pipeline, onClose }: {
  pipelineId: string; env: PipelineEnv; pipeline: Pipeline; onClose: () => void;
}) {
  const qc = useQueryClient();
  const idx = pipeline.envs.findIndex((e) => e.id === env.id);
  const prevCommit = idx > 0 ? pipeline.envs[idx - 1].current_commit : "";
  const [commit, setCommit] = useState(prevCommit || "");

  // Resolve the pipeline's app -> its repo, then offer a commit picker.
  const { data: gh } = useQuery({ queryKey: ["gh-status"], queryFn: () => apiGet<GithubStatus>("/github/status") });
  const { data: sources } = useQuery({ queryKey: ["app-sources"], queryFn: () => apiGet<AppSource[]>("/app-sources") });
  const src = sources?.find((s) => s.name === pipeline.app);
  const { data: commits } = useQuery({
    queryKey: ["gh-commits", src?.repo, src?.branch], enabled: !!(gh?.connected && src?.repo),
    queryFn: () => apiGet<GhCommit[]>(`/github/commits?repo=${encodeURIComponent(src!.repo)}&branch=${encodeURIComponent(src!.branch)}`),
  });

  const mut = useMutation({
    mutationFn: () => apiPost(`/pipelines/${pipelineId}/promote`, { env_rank: env.rank, commit }),
    onSuccess: () => {
      toast.success(`Promotion queued → ${env.name}`, { description: shortSha(commit) });
      qc.invalidateQueries({ queryKey: ["pipeline", pipelineId] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onClose();
    },
    onError: (e: Error) => toast.error("Promotion failed", { description: e.message }),
  });

  return (
    <Modal title={`Deploy to ${env.name}`}
      description={`Deploys ${pipeline.app} onto ${env.site_domain} via agent ${env.server_name}.`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
        <div className="modal-body">
          {gh?.connected && commits?.length ? (
            <label className="field">Pick a commit to ship
              <div className="commit-pick">
                {commits.map((c) => (
                  <div key={c.sha} className={`commit-opt ${commit === c.sha ? "sel" : ""}`} onClick={() => setCommit(c.sha)}>
                    <span className="co-sha">{c.sha.slice(0, 7)}</span>
                    <span>
                      <div className="co-msg">{c.message}</div>
                      <div className="co-meta">{c.author} · {new Date(c.date).toLocaleDateString()}</div>
                    </span>
                  </div>
                ))}
              </div>
            </label>
          ) : (
            <>
              <label className="field">Commit (40-char git SHA)
                <input type="text" value={commit} onChange={(e) => setCommit(e.target.value)} placeholder="full commit sha" className="mono" autoFocus required />
              </label>
              {!gh?.connected && <div className="muted" style={{ fontSize: 12 }}>Connect GitHub on the App sources page to pick commits from a list.</div>}
            </>
          )}
          {prevCommit && <div className="muted" style={{ fontSize: 12 }}>Previous env is at <span className="mono">{shortSha(prevCommit)}</span>.</div>}
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending} disabled={!commit}>Approve &amp; ship</Button>
        </div>
      </form>
    </Modal>
  );
}
