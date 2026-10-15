import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, BenchApp, BenchDetail, BenchSite, GhBranch, GhCommit, GhRepo, GithubStatus } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox } from "../ui";
import { NewSiteModal } from "../components/NewSiteModal";


type BenchTab = "sites" | "apps" | "details";

function short(sha: string) { return sha ? sha.slice(0, 7) : "—"; }
function isSemver(s: string) { return /^\d+\.\d+/.test(s); }

export function BenchDetailPage({ role }: { role?: string }) {
  const { id } = useParams();
  const nav = useNavigate();
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

  const [showNewSite, setShowNewSite] = useState(false);
  const [showInstall, setShowInstall] = useState(false);
  const [updateApp, setUpdateApp] = useState<BenchApp | null>(null);
  const [removeApp, setRemoveApp] = useState<BenchApp | null>(null);

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
            {tab === "apps" && canDeploy && <Button size="sm" onClick={() => setShowInstall(true)}>+ Install app</Button>}
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
                  {canDeploy && <th />}
                </tr>
              </thead>
              <tbody>
                {bench.apps.map((a) => (
                  <AppRow
                    key={a.name}
                    app={a}
                    ghConnected={!!gh?.connected}
                    canDeploy={canDeploy}
                    onUpdate={() => setUpdateApp(a)}
                    onRemove={() => setRemoveApp(a)}
                  />
                ))}
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

      {showNewSite && (
        <NewSiteModal benchId={bench.id} onClose={() => setShowNewSite(false)} />
      )}
      {showInstall && (
        <InstallAppModal
          bench={bench}
          connected={!!gh?.connected}
          onClose={() => setShowInstall(false)}
        />
      )}
      {updateApp && (
        <UpdateAppModal bench={bench} app={updateApp} onClose={() => setUpdateApp(null)} />
      )}
      {removeApp && (
        <RemoveAppModal bench={bench} app={removeApp} onClose={() => setRemoveApp(null)} />
      )}
    </div>
  );
}

function AppRow({ app, ghConnected, canDeploy, onUpdate, onRemove }: {
  app: BenchApp; ghConnected: boolean; canDeploy: boolean;
  onUpdate: () => void; onRemove: () => void;
}) {
  const cur = app.current_commit;              // __version__ for display ("0.0.1")
  const benchSha = app.bench_commit;           // real git HEAD for comparison
  const latest = app.latest_commit;

  // "On bench" shows the __version__ string when present, else the short SHA.
  const curIsSemver = cur && isSemver(cur);
  const latestIsSemver = latest && isSemver(latest);
  // Behind = the bench's git HEAD differs from the remote's latest commit.
  const behind = !!benchSha && !!latest && benchSha !== latest;
  const canUpdate = canDeploy && !!app.branch && app.name !== "frappe";

  return (
    <tr>
      <td style={{ fontWeight: 550 }}>{app.name}</td>
      <td className="mono muted" style={{ fontSize: 12 }}>
        {app.branch || <span className="muted">—</span>}
      </td>
      <td>
        {cur
          ? <span className="vtag-cur mono" title={benchSha || cur}>{curIsSemver ? cur : short(cur)}</span>
          : benchSha
          ? <span className="vtag-cur mono" title={benchSha}>{short(benchSha)}</span>
          : <span className="muted">unknown</span>}
      </td>
      <td>
        {latest
          ? behind
            ? <span className="vtag-behind mono" title={latest}>{latestIsSemver ? latest : short(latest)} ↑</span>
            : <span className="uptodate mono">{latestIsSemver ? latest : short(latest)} ✓</span>
          : app.repo && app.branch
            ? <span className="muted">{ghConnected ? "checking…" : <Link to="/settings">connect GitHub</Link>}</span>
            : <span className="muted">—</span>}
      </td>
      <td>
        {!app.repo ? (
          <span className="muted" style={{ fontSize: 12 }}>no remote</span>
        ) : behind ? (
          <Badge status="warning" />
        ) : (
          <Badge status="active" />
        )}
      </td>
      <td className="muted" style={{ fontSize: 12 }}>
        {app.repo
          ? <a className="mono link" href={`https://github.com/${app.repo}`} target="_blank" rel="noreferrer">{app.repo}</a>
          : <span>—</span>}
      </td>
      {canDeploy && (
        <td className="row-actions" style={{ whiteSpace: "nowrap" }}>
          <Button size="sm" variant={behind ? "primary" : "secondary"} disabled={!canUpdate}
            title={canUpdate ? "Update this app" : "No branch known for this app"}
            onClick={onUpdate}>
            {behind ? "Update ↑" : "Update"}
          </Button>{" "}
          <Button size="sm" variant="ghost" onClick={onRemove}>Remove</Button>
        </td>
      )}
    </tr>
  );
}

// UpdateAppModal: update an existing app on the bench. Lets the operator pick a
// target commit (default: latest on the remote branch) and toggle maintenance
// mode and skip-failing-patches. Runs across every active site on the bench.
function UpdateAppModal({ bench, app, onClose }: { bench: BenchDetail; app: BenchApp; onClose: () => void }) {
  const qc = useQueryClient();
  const [commit, setCommit] = useState("");      // "" = latest on branch
  const [maintenance, setMaintenance] = useState(true);
  const [skipPatches, setSkipPatches] = useState(false);
  const [hasPackage, setHasPackage] = useState(false);

  // Recent commits on the branch so the operator can pick an exact target.
  const { data: commits } = useQuery({
    queryKey: ["gh-commits", app.repo, app.branch],
    enabled: !!app.repo && !!app.branch,
    queryFn: () => apiGet<GhCommit[]>(`/github/commits?repo=${encodeURIComponent(app.repo)}&branch=${encodeURIComponent(app.branch)}`),
  });

  const activeSites = bench.sites.filter((s) => s.status === "active").map((s) => s.domain);

  const mut = useMutation({
    mutationFn: () => apiPost<{ deploy_id: number }>(`/benches/${bench.id}/apps/${app.name}/update`, {
      commit,
      has_package_json: hasPackage,
      maintenance,
      skip_failing_patches: skipPatches,
    }),
    onSuccess: (d) => {
      toast.success(`Updating ${app.name}`, { description: `Deploy #${d.deploy_id} queued` });
      qc.invalidateQueries({ queryKey: ["bench"] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onClose();
    },
    onError: (e: Error) => toast.error("Update failed to start", { description: e.message }),
  });

  return (
    <Modal title={`Update ${app.name}`} description={`${app.repo || "—"} · ${app.branch || "no branch"}`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
        <div className="modal-body">
          {!app.branch && (
            <div className="note" style={{ marginBottom: 8 }}>
              <span className="muted">No branch is known for this app — the panel could not read its git checkout, so it can't resolve commits to update to.</span>
            </div>
          )}
          <label className="field">Target commit
            <select value={commit} onChange={(e) => setCommit(e.target.value)}>
              <option value="">Latest on {app.branch || "branch"}</option>
              {(commits ?? []).map((c) => (
                <option key={c.sha} value={c.sha}>{c.sha.slice(0, 8)} — {c.message.split("\n")[0].slice(0, 60)}</option>
              ))}
            </select>
          </label>
          <label className="check"><input type="checkbox" checked={maintenance} onChange={(e) => setMaintenance(e.target.checked)} /> Maintenance mode during migrate (recommended)</label>
          <label className="check"><input type="checkbox" checked={skipPatches} onChange={(e) => setSkipPatches(e.target.checked)} /> Skip failing patches (<span className="mono">bench migrate --skip-failing</span>)</label>
          <label className="check"><input type="checkbox" checked={hasPackage} onChange={(e) => setHasPackage(e.target.checked)} /> App has a package.json (run yarn install)</label>
          <div className="note" style={{ marginTop: 10 }}>
            <span className="muted">
              Backs up, fetches, checks out, migrates, builds and restarts across{" "}
              <b>{activeSites.length}</b> active site{activeSites.length === 1 ? "" : "s"} on this bench
              ({activeSites.join(", ") || "none"}). A rollback point is recorded automatically.
            </span>
          </div>
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending} disabled={!app.branch || activeSites.length === 0}>Start update</Button>
        </div>
      </form>
    </Modal>
  );
}

// One app row in the install wizard. source="bench" installs an app already on
// the bench; "github" fetches via an owner/name repo picker; "url" fetches via a
// pasted git clone URL.
type AppRowSpec = {
  key: number;
  source: "bench" | "github" | "url";
  app: string;     // app name (what `apps/<app>` becomes)
  repo: string;    // owner/name (github source)
  url: string;     // full clone URL (url source)
  branch: string;
};

let appRowSeq = 1;
function blankRow(source: AppRowSpec["source"], onBench: string[]): AppRowSpec {
  return { key: appRowSeq++, source, app: source === "bench" ? (onBench[0] ?? "") : "", repo: "", url: "", branch: "main" };
}

// InstallAppModal: install one or more apps onto one or more sites. Each app can
// come from the bench (already fetched), a GitHub repo, or a pasted git URL.
function InstallAppModal({ bench, connected, onClose }: { bench: BenchDetail; connected: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const onBench = bench.apps.map((a) => a.name);
  const benchSet = new Set(onBench);
  const activeSites: BenchSite[] = bench.sites.filter((s) => s.status === "active");

  const [siteIds, setSiteIds] = useState<number[]>(activeSites.length ? [activeSites[0].id] : []);
  const [rows, setRows] = useState<AppRowSpec[]>([blankRow(onBench.length ? "bench" : "github", onBench)]);

  const { data: repos } = useQuery({ queryKey: ["gh-repos"], queryFn: () => apiGet<GhRepo[]>("/github/repos"), enabled: connected });

  const toggleSite = (id: number) =>
    setSiteIds((prev) => prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]);
  const allSites = () => setSiteIds(activeSites.map((s) => s.id));
  const setRow = (key: number, patch: Partial<AppRowSpec>) =>
    setRows((prev) => prev.map((r) => r.key === key ? { ...r, ...patch } : r));
  const addRow = () => setRows((prev) => [...prev, blankRow(onBench.length ? "bench" : "github", onBench)]);
  const removeRow = (key: number) => setRows((prev) => prev.filter((r) => r.key !== key));

  const payloadApps = rows.map((r) => {
    if (r.source === "bench") return { app: r.app, repo: "", url: "", branch: "" };
    if (r.source === "url")   return { app: r.app, repo: "", url: r.url.trim(), branch: r.branch };
    return { app: r.app, repo: r.repo, url: "", branch: r.branch };
  });

  const rowValid = (r: AppRowSpec) =>
    r.source === "bench" ? !!r.app
    : r.source === "url"  ? (!!r.app && !!r.url.trim() && !!r.branch)
    :                       (!!r.app && !!r.repo && !!r.branch);
  const valid = siteIds.length > 0 && rows.length > 0 && rows.every(rowValid);

  const mut = useMutation({
    mutationFn: () => apiPost<{ deploy_id: number; skipped_fetch: string[] | null }>(
      `/benches/${bench.id}/apps/install`, { site_ids: siteIds, apps: payloadApps }),
    onSuccess: (d) => {
      const skipped = d.skipped_fetch ?? [];
      toast.success("Install queued", {
        description: skipped.length
          ? `Deploy #${d.deploy_id} · already on bench (source ignored): ${skipped.join(", ")}`
          : `Deploy #${d.deploy_id}`,
      });
      qc.invalidateQueries({ queryKey: ["bench"] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onClose();
    },
    onError: (e: Error) => toast.error("Install failed to start", { description: e.message }),
  });

  return (
    <Modal title="Install apps" description={`On ${bench.name}`} onClose={onClose} wide>
      <form onSubmit={(e) => { e.preventDefault(); if (valid) mut.mutate(); }}>
        <div className="modal-body">
          <div className="field">
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <span>Sites ({siteIds.length} selected)</span>
              <button type="button" className="link" onClick={allSites} style={{ fontSize: 12 }}>select all</button>
            </div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: "6px 14px", marginTop: 6 }}>
              {activeSites.length === 0 && <span className="muted" style={{ fontSize: 12 }}>No active sites on this bench.</span>}
              {activeSites.map((s) => (
                <label key={s.id} className="check" style={{ margin: 0 }}>
                  <input type="checkbox" checked={siteIds.includes(s.id)} onChange={() => toggleSite(s.id)} /> {s.domain}
                </label>
              ))}
            </div>
          </div>

          <div className="field" style={{ marginTop: 6 }}>Apps to install</div>
          {rows.map((r) => (
            <div key={r.key} style={{ border: "1px solid var(--border)", borderRadius: 8, padding: 10, marginBottom: 8 }}>
              <div className="bench-tabs" style={{ margin: "0 0 8px" }}>
                <button type="button" className={r.source === "bench" ? "on" : ""} disabled={!onBench.length}
                  onClick={() => setRow(r.key, { source: "bench", app: onBench[0] ?? "" })}>On bench</button>
                <button type="button" className={r.source === "github" ? "on" : ""}
                  onClick={() => setRow(r.key, { source: "github" })}>GitHub repo</button>
                <button type="button" className={r.source === "url" ? "on" : ""}
                  onClick={() => setRow(r.key, { source: "url" })}>Git URL</button>
                {rows.length > 1 && (
                  <button type="button" className="link" style={{ marginLeft: "auto", fontSize: 12 }}
                    onClick={() => removeRow(r.key)}>remove</button>
                )}
              </div>

              {r.source === "bench" && (
                <label className="field" style={{ margin: 0 }}>App
                  <select value={r.app} onChange={(e) => setRow(r.key, { app: e.target.value })}>
                    {onBench.map((n) => <option key={n} value={n}>{n}</option>)}
                  </select>
                </label>
              )}

              {r.source === "github" && (
                <>
                  <label className="field" style={{ margin: 0 }}>Repository
                    <RepoBranchPicker
                      connected={connected} repos={repos}
                      repo={r.repo} branch={r.branch}
                      onRepo={(repo, defBranch) => setRow(r.key, { repo, branch: defBranch || r.branch, app: r.app || repo.split("/")[1] || "" })}
                      onBranch={(b) => setRow(r.key, { branch: b })}
                    />
                  </label>
                  <label className="field" style={{ marginTop: 6 }}>App name
                    <input type="text" value={r.app} onChange={(e) => setRow(r.key, { app: e.target.value })} placeholder="erpnext" />
                  </label>
                </>
              )}

              {r.source === "url" && (
                <>
                  <label className="field" style={{ margin: 0 }}>Git URL
                    <input type="text" value={r.url} onChange={(e) => setRow(r.key, { url: e.target.value })}
                      placeholder="https://github.com/owner/name  or  git@host:owner/name.git" />
                  </label>
                  <div style={{ display: "flex", gap: 8, marginTop: 6 }}>
                    <label className="field" style={{ flex: 1, margin: 0 }}>Branch
                      <input type="text" value={r.branch} onChange={(e) => setRow(r.key, { branch: e.target.value })} placeholder="main" />
                    </label>
                    <label className="field" style={{ flex: 1, margin: 0 }}>App name
                      <input type="text" value={r.app} onChange={(e) => setRow(r.key, { app: e.target.value })} placeholder="erpnext" />
                    </label>
                  </div>
                </>
              )}

              {r.source !== "bench" && r.app && benchSet.has(r.app) && (
                <div className="note" style={{ marginTop: 6 }}>
                  <span className="muted"><b>{r.app}</b> is already on the bench — the source is ignored and the existing checkout is installed.</span>
                </div>
              )}
            </div>
          ))}

          <button type="button" className="link" onClick={addRow} style={{ fontSize: 13 }}>+ Add another app</button>

          <div className="note" style={{ marginTop: 10 }}>
            <span className="muted">
              New apps are fetched onto the bench (get-app) once, then every selected app is installed on each
              selected site. Apps already on the bench are installed from the existing checkout.
            </span>
          </div>
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending} disabled={!valid}>
            Install {rows.length} app{rows.length === 1 ? "" : "s"} on {siteIds.length} site{siteIds.length === 1 ? "" : "s"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

// RepoBranchPicker: repo dropdown (GitHub) + branch dropdown that loads the
// repo's branches. Falls back to free-text when GitHub is not connected.
function RepoBranchPicker({ connected, repos, repo, branch, onRepo, onBranch }: {
  connected: boolean; repos?: GhRepo[]; repo: string; branch: string;
  onRepo: (repo: string, defaultBranch?: string) => void; onBranch: (b: string) => void;
}) {
  const { data: branches } = useQuery({
    queryKey: ["gh-branches", repo], enabled: connected && !!repo,
    queryFn: () => apiGet<GhBranch[]>(`/github/branches?repo=${encodeURIComponent(repo)}`),
  });
  if (!connected) {
    return (
      <div style={{ display: "flex", gap: 8 }}>
        <input type="text" value={repo} onChange={(e) => onRepo(e.target.value)} placeholder="owner/name" style={{ flex: 2 }} />
        <input type="text" value={branch} onChange={(e) => onBranch(e.target.value)} placeholder="branch" style={{ flex: 1 }} />
      </div>
    );
  }
  return (
    <div style={{ display: "flex", gap: 8 }}>
      <select value={repo} onChange={(e) => {
        const r = repos?.find((x) => x.full_name === e.target.value);
        onRepo(e.target.value, r?.default_branch);
      }} style={{ flex: 2 }}>
        <option value="">Select a repo…</option>
        {(repos ?? []).map((r) => <option key={r.full_name} value={r.full_name}>{r.full_name}{r.private ? " (private)" : ""}</option>)}
      </select>
      <select value={branch} onChange={(e) => onBranch(e.target.value)} disabled={!repo} style={{ flex: 1 }}>
        {(branches ?? [{ name: branch, sha: "" }]).map((b) => <option key={b.name} value={b.name}>{b.name}</option>)}
      </select>
    </div>
  );
}

// RemoveAppModal: uninstall an app from one site's database. Does not delete the
// app code from the bench. Requires choosing the site; force skips the
// dependency safety check.
function RemoveAppModal({ bench, app, onClose }: { bench: BenchDetail; app: BenchApp; onClose: () => void }) {
  const qc = useQueryClient();
  // Only sites that actually have this app installed.
  const sites = bench.sites.filter((s) => s.apps.includes(app.name));
  const [siteId, setSiteId] = useState<number | "">(sites[0]?.id ?? "");
  const [force, setForce] = useState(false);
  const [confirm, setConfirm] = useState("");

  const mut = useMutation({
    mutationFn: () => apiPost<{ deploy_id: number }>(`/sites/${siteId}/apps/${app.name}/uninstall`, { force }),
    onSuccess: (d) => {
      toast.success(`Uninstalling ${app.name}`, { description: `Deploy #${d.deploy_id}` });
      qc.invalidateQueries({ queryKey: ["bench"] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onClose();
    },
    onError: (e: Error) => toast.error("Uninstall failed to start", { description: e.message }),
  });

  const site = sites.find((s) => s.id === siteId);
  const valid = siteId !== "" && confirm === app.name;

  return (
    <Modal title={`Remove ${app.name}`} description="Uninstall from a site (app code stays on the bench)" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); if (valid) mut.mutate(); }}>
        <div className="modal-body">
          {sites.length === 0 ? (
            <div className="note"><span className="muted">No site on this bench has <b>{app.name}</b> installed.</span></div>
          ) : (
            <>
              <label className="field">Site
                <select value={siteId} onChange={(e) => setSiteId(e.target.value ? Number(e.target.value) : "")} required>
                  <option value="">Select a site…</option>
                  {sites.map((s) => <option key={s.id} value={s.id}>{s.domain}</option>)}
                </select>
              </label>
              <label className="check"><input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} /> Force (skip dependency / linked-document check)</label>
              <label className="field" style={{ marginTop: 8 }}>
                Type <b>{app.name}</b> to confirm
                <input type="text" value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder={app.name} autoComplete="off" />
              </label>
              <div className="note" style={{ marginTop: 6 }}>
                <span className="muted">
                  This runs <span className="mono">bench --site {site?.domain ?? "<site>"} uninstall-app {app.name} --yes</span> and
                  drops the app's tables &amp; data on that site. It cannot be undone without a restore.
                </span>
              </div>
            </>
          )}
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="danger" loading={mut.isPending} disabled={!valid}>Uninstall</Button>
        </div>
      </form>
    </Modal>
  );
}

