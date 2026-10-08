import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, BenchApp, BenchDetail, GhBranch, GhRepo, GithubStatus } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox } from "../ui";
import { NewSiteModal } from "../components/NewSiteModal";


type BenchTab = "sites" | "apps" | "details";

function short(sha: string) { return sha ? sha.slice(0, 7) : "—"; }

export function BenchDetailPage({ role }: { role?: string }) {
  const { id } = useParams();
  const nav = useNavigate();
  const canAdmin = atLeast(role ?? "", "admin");
  const canDeploy = atLeast(role ?? "", "deployer");
  const [tab, setTab] = useState<BenchTab>("sites");

  const { data: bench, isLoading } = useQuery({
    queryKey: ["bench", id],
    queryFn: () => apiGet<BenchDetail>(`/benches/${id}`),
    refetchInterval: 30_000,
  });
  const { data: gh } = useQuery({
    queryKey: ["gh-status"],
    queryFn: () => apiGet<GithubStatus>("/github/status"),
  });

  const [showAddSource, setShowAddSource] = useState(false);
  const [showNewSite, setShowNewSite] = useState(false);

  if (isLoading || !bench) return <PageHeader title="Loading…" />;

  const f = bench.facts || {};
  const online = bench.server_status === "online";

  return (
    <div className="stack">
      <PageHeader
        title={bench.name}
        sub={
          <span>
            <span className={`bh-sdot ${online ? "on" : "off"}`} style={{ marginRight: 6 }} />
            {bench.server_name} · frappe {bench.frappe_version || "—"} · {bench.env || "unknown env"}
          </span>
        }
        action={
          <div className="toolbar">
            {tab === "sites" && canDeploy && <Button variant="secondary" onClick={() => setShowNewSite(true)}>+ New site</Button>}
            {tab === "apps" && canAdmin && <Button size="sm" onClick={() => setShowAddSource(true)}>Add app source</Button>}
          </div>
        }
      />

      <div className="bench-tabs">
        <button className={tab === "sites" ? "on" : ""} onClick={() => setTab("sites")}>
          Sites <span className="bench-tab-count">{bench.sites.length}</span>
        </button>
        <button className={tab === "apps" ? "on" : ""} onClick={() => setTab("apps")}>
          Apps <span className="bench-tab-count">{bench.apps.length}</span>
        </button>
        <button className={tab === "details" ? "on" : ""} onClick={() => setTab("details")}>
          Details
        </button>
      </div>

      {tab === "sites" && (
        <PanelBox>
          {bench.sites.length === 0 ? (
            <Empty>No sites on this bench yet.</Empty>
          ) : (
            <table>
              <thead><tr><th>Domain</th><th>Apps</th><th>SSL</th><th>Status</th><th /></tr></thead>
              <tbody>
                {bench.sites.map((s) => (
                  <tr key={s.id} style={{ cursor: "pointer" }} onClick={() => nav(`/sites/${s.id}`)}>
                    <td style={{ fontWeight: 550 }}>{s.domain}</td>
                    <td className="muted" style={{ fontSize: 12 }}>{s.apps.join(", ") || "—"}</td>
                    <td>{s.ssl ? <span className="badge active">on</span> : <span className="badge muted">off</span>}</td>
                    <td><Badge status={s.status === "active" ? "active" : s.status} /></td>
                    <td className="row-actions"><span style={{ color: "var(--text-faint)" }}>→</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {canDeploy && (
            <div style={{ padding: "10px 14px" }}>
              <Button variant="secondary" size="sm" onClick={() => setShowNewSite(true)}>+ New site on {bench.name}</Button>
            </div>
          )}
        </PanelBox>
      )}

      {tab === "apps" && (
        <PanelBox>
          {!bench.apps.length ? (
            <Empty>No apps recorded. Run a server refresh to discover them.</Empty>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>App</th>
                  <th>Branch</th>
                  <th>On bench</th>
                  <th>Latest in repo</th>
                  <th>Status</th>
                  <th>Source</th>
                </tr>
              </thead>
              <tbody>
                {bench.apps.map((a) => <AppRow key={a.name} app={a} ghConnected={!!gh?.connected} />)}
              </tbody>
            </table>
          )}
        </PanelBox>
      )}

      {tab === "details" && (
        <PanelBox>
          <div className="bench-facts-row" style={{ padding: "14px 16px" }}>
            <div className="bf"><div className="k">Path</div><div className="v mono" style={{ fontSize: 12, color: "var(--text-faint)" }}>{bench.path}</div></div>
            <div className="bf"><div className="k">Runtime</div><div className="v">{f.python_version ? `Python ${f.python_version}` : "—"}{f.node_version ? ` · Node ${f.node_version}` : ""}</div></div>
            <div className="bf"><div className="k">Database</div><div className="v">{f.db_type || "—"}</div></div>
            <div className="bf"><div className="k">Workers</div><div className="v">{(f.web_workers || f.rq_workers) ? `${f.web_workers ?? 0} web · ${f.rq_workers ?? 0} rq` : "—"}</div></div>
            <div className="bf"><div className="k">Scheduler</div><div className="v">{f.scheduler_on === undefined ? "—" : f.scheduler_on ? "On" : "Off"}</div></div>
            <div className="bf"><div className="k">Server</div><div className="v"><Link to="/fleet" className="link">{bench.server_name}</Link></div></div>
          </div>
        </PanelBox>
      )}

      <Link to="/benches" className="muted" style={{ fontSize: 13 }}>← Back to benches & sites</Link>

      {showAddSource && (
        <AddSourceModal
          connected={!!gh?.connected}
          benchName={bench.name}
          onClose={() => setShowAddSource(false)}
        />
      )}
      {showNewSite && (
        <NewSiteModal benchId={bench.id} onClose={() => setShowNewSite(false)} />
      )}
    </div>
  );
}

function AppRow({ app, ghConnected }: { app: BenchApp; ghConnected: boolean }) {
  const cur = app.current_commit;
  const latest = app.latest_commit;
  const behind = cur && latest && cur !== latest;
  const linked = !!app.app_source_id;

  return (
    <tr>
      <td style={{ fontWeight: 550 }}>{app.name}</td>
      <td className="mono muted" style={{ fontSize: 12 }}>{app.branch || (linked ? "—" : <span className="muted">no source</span>)}</td>
      <td>
        {cur
          ? <span className="vtag-cur mono">{short(cur)}</span>
          : <span className="muted">unknown</span>}
      </td>
      <td>
        {latest
          ? behind
            ? <span className="vtag-behind mono" title={latest}>{short(latest)} ↑ update available</span>
            : <span className="uptodate mono">{short(latest)} ✓ up to date</span>
          : <span className="muted">{ghConnected ? "—" : <Link to="/settings">connect GitHub in Settings</Link>}</span>}
      </td>
      <td>
        {!linked
          ? <span className="badge" style={{ background: "var(--surface-2)", color: "var(--text-faint)", fontSize: 11 }}>no source</span>
          : behind
          ? <Badge status="warning" />
          : <Badge status="active" />}
      </td>
      <td className="muted" style={{ fontSize: 12 }}>
        {app.repo
          ? <span className="mono">{app.repo.split("/")[1] ?? app.repo} · {app.kind}</span>
          : <span>—</span>}
      </td>
    </tr>
  );
}

function AddSourceModal({ connected, benchName, onClose }: { connected: boolean; benchName: string; onClose: () => void }) {
  const qc = useQueryClient();
  const [f, setF] = useState({ name: "", repo: "", branch: "main", kind: "frappe", auto_deploy: true });
  const set = (k: string, v: unknown) => setF((p) => ({ ...p, [k]: v }));

  const { data: repos } = useQuery({ queryKey: ["gh-repos"], queryFn: () => apiGet<GhRepo[]>("/github/repos"), enabled: connected });
  const { data: branches } = useQuery({
    queryKey: ["gh-branches", f.repo], enabled: connected && !!f.repo,
    queryFn: () => apiGet<GhBranch[]>(`/github/branches?repo=${encodeURIComponent(f.repo)}`),
  });
  useEffect(() => {
    if (!f.repo) return;
    const r = repos?.find((x) => x.full_name === f.repo);
    setF((p) => ({ ...p, name: p.name || f.repo.split("/")[1] || "", branch: r?.default_branch || p.branch }));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [f.repo]);

  const mut = useMutation({
    mutationFn: () => apiPost("/app-sources", f),
    onSuccess: () => {
      toast.success("App source added");
      qc.invalidateQueries({ queryKey: ["app-sources"] });
      qc.invalidateQueries({ queryKey: ["bench"] });
      onClose();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <Modal title="Add app source" description={`Register a GitHub repo to track on ${benchName}`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
        <div className="modal-body">
          {connected ? (
            <>
              <label className="field">Repository
                <select value={f.repo} onChange={(e) => set("repo", e.target.value)} required>
                  <option value="">Select a repo…</option>
                  {(repos ?? []).map((r) => <option key={r.full_name} value={r.full_name}>{r.full_name}{r.private ? " (private)" : ""}</option>)}
                </select>
              </label>
              <label className="field">Branch
                <select value={f.branch} onChange={(e) => set("branch", e.target.value)} disabled={!f.repo}>
                  {(branches ?? [{ name: f.branch, sha: "" }]).map((b) => <option key={b.name} value={b.name}>{b.name}</option>)}
                </select>
              </label>
            </>
          ) : (
            <>
              <label className="field">Repo (owner/name)<input type="text" value={f.repo} onChange={(e) => set("repo", e.target.value)} placeholder="myorg/erpnext" required /></label>
              <label className="field">Branch<input type="text" value={f.branch} onChange={(e) => set("branch", e.target.value)} /></label>
            </>
          )}
          <label className="field">App name<input type="text" value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="erpnext" required /></label>
          <label className="field">Kind
            <select value={f.kind} onChange={(e) => set("kind", e.target.value)}>
              <option value="frappe">frappe</option>
              <option value="web">web</option>
            </select>
          </label>
          <label className="check"><input type="checkbox" checked={f.auto_deploy} onChange={(e) => set("auto_deploy", e.target.checked)} /> Auto-deploy on push</label>
          <div className="note" style={{ marginTop: 8 }}>
            {!connected && <span className="muted">Connect GitHub in Settings to browse repos and track versions.</span>}
          </div>
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending}>Add source</Button>
        </div>
      </form>
    </Modal>
  );
}

