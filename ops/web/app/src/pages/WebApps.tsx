import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  apiDelete, apiGet, apiPost, atLeast,
  GhBranch, GhRepo, GithubStatus, Server, WebApp,
} from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox, TableSkeleton } from "../ui";

const RUNNING = ["new", "deploying"];
function short(s: string) { return s ? s.slice(0, 7) : "—"; }

// commitCell shows the deployed commit vs. the latest remote head:
//  - up to date  → green sha ✓
//  - behind      → amber "deployed → latest"
//  - not deployed → "— → latest"
//  - no GitHub / unknown latest → just the deployed sha
function commitCell(app: WebApp) {
  const cur = short(app.commit);
  if (!app.latest_commit) return <span className="mono" title={app.commit}>{cur}</span>;
  const latest = short(app.latest_commit);
  if (app.up_to_date) {
    return <span className="mono" style={{ color: "#3fb950" }} title={`up to date (${app.commit})`}>{cur} ✓</span>;
  }
  return (
    <span className="mono" style={{ color: "#d08b1f" }} title={`deployed ${app.commit || "(none)"} → latest ${app.latest_commit}`}>
      {cur} → {latest}
    </span>
  );
}

export function WebAppsPage({ role }: { role?: string }) {
  const qc = useQueryClient();
  const canDeploy = atLeast(role ?? "", "deployer");
  const canAdmin = atLeast(role ?? "", "admin");
  const { data, isLoading } = useQuery({
    queryKey: ["web-apps"], queryFn: () => apiGet<WebApp[]>("/web-apps"),
    refetchInterval: (q) => (q.state.data as WebApp[] | undefined)?.some((a) => RUNNING.includes(a.status)) ? 3000 : 15000,
  });
  const { data: gh } = useQuery({ queryKey: ["gh-status"], queryFn: () => apiGet<GithubStatus>("/github/status") });
  const [showNew, setShowNew] = useState(false);

  return (
    <>
      <PageHeader
        title="Web apps"
        sub="Docker or PM2 apps, each in its own websites dir"
        action={canDeploy ? <Button size="sm" onClick={() => setShowNew(true)}>+ New web app</Button> : undefined}
      />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={6} /> : !data?.length ? (
          <Empty>No web apps yet.{canDeploy ? " Create one to deploy a Docker or PM2 service." : ""}</Empty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th><th>Runtime</th><th>Source</th><th>Port</th>
                <th>Route</th><th>Deployed → latest</th><th>Status</th>{canDeploy && <th />}
              </tr>
            </thead>
            <tbody>
              {data.map((a) => (
                <WebAppRow key={a.id} app={a} canDeploy={canDeploy} canAdmin={canAdmin} />
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>

      {showNew && (
        <NewWebAppModal connected={!!gh?.connected} onClose={() => setShowNew(false)} onCreated={() => {
          qc.invalidateQueries({ queryKey: ["web-apps"] });
          setShowNew(false);
        }} />
      )}
    </>
  );
}

function WebAppRow({ app, canDeploy, canAdmin }: { app: WebApp; canDeploy: boolean; canAdmin: boolean }) {
  const qc = useQueryClient();
  const invalidate = () => { qc.invalidateQueries({ queryKey: ["web-apps"] }); qc.invalidateQueries({ queryKey: ["deploys"] }); };

  const deployMut = useMutation({
    mutationFn: () => apiPost<{ deploy_id: number }>(`/web-apps/${app.id}/deploy`, { commit: "" }),
    onSuccess: (d) => { toast.success(`Deploying ${app.name}`, { description: `Deploy #${d.deploy_id}` }); invalidate(); },
    onError: (e: Error) => toast.error("Deploy failed to start", { description: e.message }),
  });
  const rollbackMut = useMutation({
    mutationFn: () => apiPost<{ deploy_id: number }>(`/web-apps/${app.id}/rollback`, {}),
    onSuccess: (d) => { toast.success(`Rolling back ${app.name}`, { description: `Deploy #${d.deploy_id}` }); invalidate(); },
    onError: (e: Error) => toast.error("Rollback failed to start", { description: e.message }),
  });
  const deleteMut = useMutation({
    mutationFn: () => apiDelete(`/web-apps/${app.id}`),
    onSuccess: () => { toast.success(`Removed ${app.name}`); invalidate(); },
    onError: (e: Error) => toast.error("Delete failed", { description: e.message }),
  });

  return (
    <tr>
      <td style={{ fontWeight: 550 }}>{app.name}</td>
      <td><span className={`badge ${app.runtime === "pm2" ? "muted" : "active"}`}>{app.runtime}</span></td>
      <td className="muted" style={{ fontSize: 12 }}>
        {app.repo
          ? <a className="mono link" href={`https://github.com/${app.repo}`} target="_blank" rel="noreferrer">{app.repo}</a>
          : app.url ? <span className="mono">{app.url}</span> : "—"}
        <span className="muted"> · {app.branch}</span>
      </td>
      <td className="mono">127.0.0.1:{app.internal_port}</td>
      <td style={{ fontSize: 12 }}>
        {app.route_domain
          ? <a className="link mono" href="#/routes" title={`route #${app.route_id} (${app.route_status})`}>{app.route_domain}</a>
          : <span className="muted">— not routed —</span>}
      </td>
      <td style={{ fontSize: 12 }}>{commitCell(app)}</td>
      <td><Badge status={app.status} /></td>
      {canDeploy && (
        <td className="row-actions" style={{ whiteSpace: "nowrap" }}>
          <Button size="sm" variant="secondary" loading={deployMut.isPending} onClick={() => deployMut.mutate()}>Deploy latest</Button>{" "}
          {app.previous_commit && (
            <Button size="sm" variant="ghost" loading={rollbackMut.isPending} onClick={() => rollbackMut.mutate()} title={`Roll back to ${short(app.previous_commit)}`}>Rollback</Button>
          )}{" "}
          {canAdmin && (
            <Button size="sm" variant="ghost" loading={deleteMut.isPending}
              onClick={() => { if (confirm(`Remove web app "${app.name}"? This deletes its record (the running service is left as-is).`)) deleteMut.mutate(); }}>
              Delete
            </Button>
          )}
        </td>
      )}
    </tr>
  );
}

function NewWebAppModal({ connected, onClose, onCreated }: { connected: boolean; onClose: () => void; onCreated: () => void }) {
  const { data: servers } = useQuery({ queryKey: ["servers"], queryFn: () => apiGet<Server[]>("/servers") });
  const { data: repos } = useQuery({ queryKey: ["gh-repos"], queryFn: () => apiGet<GhRepo[]>("/github/repos"), enabled: connected });

  const [f, setF] = useState({
    server_id: 0,
    name: "",
    runtime: "docker" as "docker" | "pm2",
    source: "github" as "github" | "url",
    repo: "",
    url: "",
    branch: "main",
    internal_port: 3000,
    container_port: 3000,
    health_path: "/",
    dockerfile_path: "",
    start_command: "",
    build_command: "",
    env: "",
    auto_deploy: false,
  });
  const set = (k: string, v: unknown) => setF((p) => ({ ...p, [k]: v }));

  useEffect(() => {
    if (servers?.length && f.server_id === 0) set("server_id", servers[0].id);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [servers]);

  const { data: branches } = useQuery({
    queryKey: ["gh-branches", f.repo], enabled: connected && f.source === "github" && !!f.repo,
    queryFn: () => apiGet<GhBranch[]>(`/github/branches?repo=${encodeURIComponent(f.repo)}`),
  });
  useEffect(() => {
    if (f.source !== "github" || !f.repo) return;
    const r = repos?.find((x) => x.full_name === f.repo);
    setF((p) => ({ ...p, name: p.name || f.repo.split("/")[1] || "", branch: r?.default_branch || p.branch }));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [f.repo]);

  // Auto-detect Dockerfiles once a GitHub repo+branch is chosen (docker runtime).
  const dfEnabled = connected && f.runtime === "docker" && f.source === "github" && !!f.repo && !!f.branch;
  const { data: dockerfiles, isFetching: dfLoading } = useQuery({
    queryKey: ["gh-dockerfiles", f.repo, f.branch], enabled: dfEnabled,
    queryFn: () => apiGet<string[]>(`/github/dockerfiles?repo=${encodeURIComponent(f.repo)}&branch=${encodeURIComponent(f.branch)}`),
  });
  const noDockerfile = dfEnabled && !dfLoading && Array.isArray(dockerfiles) && dockerfiles.length === 0;
  // Default the dockerfile selection to the first candidate once detected.
  useEffect(() => {
    if (dockerfiles && dockerfiles.length && !f.dockerfile_path) set("dockerfile_path", dockerfiles[0]);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dockerfiles]);

  const mut = useMutation({
    mutationFn: () => apiPost<{ id: number; deploy_id?: number; deploy_error?: string }>("/web-apps", {
      server_id: f.server_id,
      name: f.name,
      runtime: f.runtime,
      repo: f.source === "github" ? f.repo : "",
      url: f.source === "url" ? f.url.trim() : "",
      branch: f.branch,
      internal_port: Number(f.internal_port),
      container_port: Number(f.container_port),
      health_path: f.health_path || "/",
      dockerfile_path: f.runtime === "docker" ? f.dockerfile_path : "",
      start_command: f.start_command,
      build_command: f.build_command,
      auto_deploy: f.auto_deploy,
      env: f.env,
      deploy: true,
    }),
    onSuccess: (d) => {
      if (d.deploy_error) toast.warning("Created, but deploy did not start", { description: d.deploy_error });
      else toast.success("Web app created & deploying", { description: `Deploy #${d.deploy_id}` });
      onCreated();
    },
    onError: (e: Error) => toast.error("Could not create web app", { description: e.message }),
  });

  const valid = f.server_id > 0 && !!f.name &&
    (f.source === "github" ? !!f.repo : !!f.url.trim()) &&
    !!f.branch && Number(f.internal_port) > 0;

  return (
    <Modal title="New web app" description="Deploy a Docker or PM2 service from a git repo" onClose={onClose} wide>
      <form onSubmit={(e) => { e.preventDefault(); if (valid) mut.mutate(); }}>
        <div className="modal-body">
          <label className="field">Server
            <select value={f.server_id} onChange={(e) => set("server_id", Number(e.target.value))} required>
              {!servers?.length && <option value={0}>No servers</option>}
              {(servers ?? []).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          </label>

          <label className="field">Runtime
            <div className="bench-tabs" style={{ marginTop: 4 }}>
              <button type="button" className={f.runtime === "docker" ? "on" : ""} onClick={() => set("runtime", "docker")}>Docker</button>
              <button type="button" className={f.runtime === "pm2" ? "on" : ""} onClick={() => set("runtime", "pm2")}>PM2 (node)</button>
            </div>
          </label>

          <div className="bench-tabs" style={{ margin: "4px 0 10px" }}>
            <button type="button" className={f.source === "github" ? "on" : ""} onClick={() => set("source", "github")}>GitHub repo</button>
            <button type="button" className={f.source === "url" ? "on" : ""} onClick={() => set("source", "url")}>Git URL</button>
          </div>
          {f.source === "github" ? (
            connected ? (
              <div style={{ display: "flex", gap: 8 }}>
                <label className="field" style={{ flex: 2, margin: 0 }}>Repository
                  <select value={f.repo} onChange={(e) => set("repo", e.target.value)} required>
                    <option value="">Select a repo…</option>
                    {(repos ?? []).map((r) => <option key={r.full_name} value={r.full_name}>{r.full_name}{r.private ? " (private)" : ""}</option>)}
                  </select>
                </label>
                <label className="field" style={{ flex: 1, margin: 0 }}>Branch
                  <select value={f.branch} onChange={(e) => set("branch", e.target.value)} disabled={!f.repo}>
                    {(branches ?? [{ name: f.branch, sha: "" }]).map((b) => <option key={b.name} value={b.name}>{b.name}</option>)}
                  </select>
                </label>
              </div>
            ) : (
              <div style={{ display: "flex", gap: 8 }}>
                <label className="field" style={{ flex: 2, margin: 0 }}>Repo (owner/name)<input type="text" value={f.repo} onChange={(e) => set("repo", e.target.value)} placeholder="myorg/app" /></label>
                <label className="field" style={{ flex: 1, margin: 0 }}>Branch<input type="text" value={f.branch} onChange={(e) => set("branch", e.target.value)} /></label>
              </div>
            )
          ) : (
            <div style={{ display: "flex", gap: 8 }}>
              <label className="field" style={{ flex: 2, margin: 0 }}>Git URL<input type="text" value={f.url} onChange={(e) => set("url", e.target.value)} placeholder="https://host/owner/app.git" /></label>
              <label className="field" style={{ flex: 1, margin: 0 }}>Branch<input type="text" value={f.branch} onChange={(e) => set("branch", e.target.value)} /></label>
            </div>
          )}

          <label className="field">App name<input type="text" value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="my-app" required /></label>

          <div style={{ display: "flex", gap: 8 }}>
            <label className="field" style={{ flex: 1, margin: 0 }}>Internal port (127.0.0.1)
              <input type="number" value={f.internal_port} onChange={(e) => set("internal_port", e.target.value)} required />
            </label>
            {f.runtime === "docker" && (
              <label className="field" style={{ flex: 1, margin: 0 }}>Container port
                <input type="number" value={f.container_port} onChange={(e) => set("container_port", e.target.value)} />
              </label>
            )}
            <label className="field" style={{ flex: 1, margin: 0 }}>Health path
              <input type="text" value={f.health_path} onChange={(e) => set("health_path", e.target.value)} placeholder="/" />
            </label>
          </div>

          {f.runtime === "docker" && (
            <label className="field">Dockerfile
              {f.source === "github" && connected ? (
                dfLoading ? (
                  <select disabled><option>Scanning repo…</option></select>
                ) : noDockerfile ? (
                  <input type="text" value={f.dockerfile_path} onChange={(e) => set("dockerfile_path", e.target.value)} placeholder="Dockerfile (none found in repo)" />
                ) : (
                  <select value={f.dockerfile_path} onChange={(e) => set("dockerfile_path", e.target.value)}>
                    {!f.repo && <option value="">Select a repo first…</option>}
                    {(dockerfiles ?? []).map((d) => <option key={d} value={d}>{d}</option>)}
                  </select>
                )
              ) : (
                <input type="text" value={f.dockerfile_path} onChange={(e) => set("dockerfile_path", e.target.value)} placeholder="Dockerfile (path relative to repo root)" />
              )}
            </label>
          )}

          {noDockerfile && (
            <div className="note" style={{ background: "var(--warn-bg)", borderRadius: 8, padding: "8px 10px" }}>
              <span>
                ⚠ No Dockerfile found in <b>{f.repo}@{f.branch}</b>. The Docker build will fail unless you add one or point to a path.
                {" "}For a plain Node app,{" "}
                <button type="button" className="link" onClick={() => set("runtime", "pm2")}>switch to PM2</button>.
              </span>
            </div>
          )}

          {f.runtime === "pm2" && (
            <div style={{ display: "flex", gap: 8 }}>
              <label className="field" style={{ flex: 1, margin: 0 }}>Build command
                <input type="text" value={f.build_command} onChange={(e) => set("build_command", e.target.value)} placeholder="npm run build (default; '-' to skip)" />
              </label>
              <label className="field" style={{ flex: 1, margin: 0 }}>Start command
                <input type="text" value={f.start_command} onChange={(e) => set("start_command", e.target.value)} placeholder="npm start (default)" />
              </label>
            </div>
          )}

          <label className="field">Environment (.env)
            <textarea value={f.env} onChange={(e) => set("env", e.target.value)} rows={4}
              placeholder={"KEY=value\nANOTHER=value"} style={{ fontFamily: "var(--font-mono)", fontSize: 12 }} />
          </label>

          <label className="check"><input type="checkbox" checked={f.auto_deploy} onChange={(e) => set("auto_deploy", e.target.checked)} /> Auto-deploy on push (requires a matching app source + webhook)</label>

          <div className="note" style={{ marginTop: 8 }}>
            <span className="muted">
              {f.runtime === "docker"
                ? "Clones the repo, builds a Docker image, writes a generated compose.yaml bound to 127.0.0.1, brings it up and health-checks it."
                : "Clones the repo into the websites dir, runs npm install + build, writes a pm2 ecosystem config (PORT = internal port) and starts it under pm2, then health-checks it."}
            </span>
          </div>
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending} disabled={!valid}>Create &amp; deploy</Button>
        </div>
      </form>
    </Modal>
  );
}
