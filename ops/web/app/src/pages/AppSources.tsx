import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, AppSource, GhBranch, GhRepo, GithubStatus } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function AppSourcesPage({ role }: { role?: string }) {
  const { data, isLoading } = useQuery({ queryKey: ["app-sources"], queryFn: () => apiGet<AppSource[]>("/app-sources") });
  const { data: gh } = useQuery({ queryKey: ["gh-status"], queryFn: () => apiGet<GithubStatus>("/github/status") });
  const [showAdd, setShowAdd] = useState(false);
  const canAdmin = atLeast(role ?? "", "admin");

  return (
    <>
      <PageHeader title="App sources" sub="GitHub repos that back your Frappe apps" />
      <PanelBox title="Sources" action={canAdmin && <Button size="sm" onClick={() => setShowAdd(true)}>Add source</Button>}>
        {isLoading ? <TableSkeleton cols={6} /> : !data?.length ? <Empty>No app sources yet. Connect GitHub in Settings → Integrations, then add a source.</Empty> : (
          <table>
            <thead><tr><th>Name</th><th>Repo</th><th>Branch</th><th>Version</th><th>Kind</th><th>Auto-deploy</th></tr></thead>
            <tbody>
              {data.map((s) => (
                <tr key={s.id}>
                  <td style={{ fontWeight: 550 }}>{s.name}</td>
                  <td className="mono">{s.repo}</td>
                  <td className="muted">{s.branch}</td>
                  <td className="ver-badge">
                    {s.latest_commit
                      ? <span className="uptodate">{s.latest_commit.slice(0, 7)} <span className="muted">latest</span></span>
                      : <span className="muted">{gh?.connected ? "—" : "connect GitHub"}</span>}
                  </td>
                  <td><Badge status={s.kind} /></td>
                  <td className="muted">{s.auto_deploy ? "on" : "off"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
      {showAdd && <AddSourceModal connected={!!gh?.connected} onClose={() => setShowAdd(false)} />}
    </>
  );
}

function AddSourceModal({ connected, onClose }: { connected: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const [f, setF] = useState({ name: "", repo: "", branch: "main", kind: "frappe", auto_deploy: true });
  const set = (k: string, v: unknown) => setF((p) => ({ ...p, [k]: v }));

  // When connected, offer repo + branch dropdowns from GitHub.
  const { data: repos } = useQuery({ queryKey: ["gh-repos"], queryFn: () => apiGet<GhRepo[]>("/github/repos"), enabled: connected });
  const { data: branches } = useQuery({
    queryKey: ["gh-branches", f.repo], enabled: connected && !!f.repo,
    queryFn: () => apiGet<GhBranch[]>(`/github/branches?repo=${encodeURIComponent(f.repo)}`),
  });
  // Default name + branch from the chosen repo.
  useEffect(() => {
    if (!f.repo) return;
    const r = repos?.find((x) => x.full_name === f.repo);
    setF((p) => ({ ...p, name: p.name || f.repo.split("/")[1] || "", branch: r?.default_branch || p.branch }));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [f.repo]);

  const mut = useMutation({
    mutationFn: () => apiPost("/app-sources", f),
    onSuccess: () => { toast.success("App source added"); qc.invalidateQueries({ queryKey: ["app-sources"] }); onClose(); },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <Modal title="Add app source" onClose={onClose}>
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
          <label className="field">Name (app)<input type="text" value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="erpnext" required /></label>
          <label className="field">Kind
            <select value={f.kind} onChange={(e) => set("kind", e.target.value)}>
              <option value="frappe">frappe</option>
              <option value="web">web</option>
            </select>
          </label>
          <label className="check"><input type="checkbox" checked={f.auto_deploy} onChange={(e) => set("auto_deploy", e.target.checked)} /> Auto-deploy on push</label>
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending}>Add</Button>
        </div>
      </form>
    </Modal>
  );
}
