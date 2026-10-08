import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, Site } from "../api";
import { Button, Empty, Modal, PageHeader, PanelBox } from "../ui";

export function SiteDetailPage({ role }: { role?: string }) {
  const { id } = useParams();
  const qc = useQueryClient();
  const nav = useNavigate();
  const { data: site, isLoading } = useQuery({ queryKey: ["site", id], queryFn: () => apiGet<Site>(`/sites/${id}`) });
  const [showDelete, setShowDelete] = useState(false);
  const canDeploy = atLeast(role ?? "", "deployer");
  const canAdmin = atLeast(role ?? "", "admin");

  const refresh = useMutation({
    mutationFn: () => apiPost(`/sites/${id}/refresh`),
    onSuccess: () => { toast.success("Refresh queued"); qc.invalidateQueries({ queryKey: ["deploys"] }); },
    onError: (e: Error) => toast.error(e.message),
  });
  const backup = useMutation({
    mutationFn: () => apiPost(`/sites/${id}/backup`, { with_files: false }),
    onSuccess: () => { toast.success("Backup queued"); qc.invalidateQueries({ queryKey: ["deploys"] }); },
    onError: (e: Error) => toast.error(e.message),
  });
  const rollback = useMutation({
    mutationFn: (app: string) => apiPost(`/sites/${id}/apps/${app}/rollback`),
    onSuccess: () => { toast.success("Rollback queued"); qc.invalidateQueries({ queryKey: ["deploys"] }); },
    onError: (e: Error) => toast.error("Rollback failed", { description: e.message }),
  });

  if (isLoading || !site) return <PageHeader title="Loading…" />;

  return (
    <>
      <PageHeader
        title={site.domain}
        sub={`${site.status} · SSL ${site.ssl ? "on" : "off"}`}
        action={
          <div className="toolbar">
            {canAdmin && <Button variant="secondary" loading={refresh.isPending} onClick={() => refresh.mutate()}>Refresh</Button>}
            {canDeploy && <Button variant="secondary" loading={backup.isPending} onClick={() => backup.mutate()}>Backup</Button>}
            {canAdmin && <Button variant="danger" onClick={() => setShowDelete(true)}>Delete site</Button>}
          </div>
        }
      />
      <PanelBox title="Installed apps">
        {!site.apps?.length ? (
          <Empty>No apps recorded. Run a refresh to discover them.</Empty>
        ) : (
          <table>
            <thead><tr><th>App</th><th>Version</th><th></th></tr></thead>
            <tbody>
              {site.apps.map((a) => (
                <tr key={a.name}>
                  <td style={{ fontWeight: 550 }}>{a.name}</td>
                  <td className="mono">{a.version || <span className="muted">unknown</span>}</td>
                  <td className="row-actions">
                    {canDeploy && a.rollbackable && (
                      <Button variant="secondary" size="sm"
                        loading={rollback.isPending && rollback.variables === a.name}
                        onClick={() => {
                          if (confirm(`Roll back ${a.name} to the previous commit and restore the pre-deploy database? Data written since the last deploy will be lost.`))
                            rollback.mutate(a.name);
                        }}>
                        Roll back
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
      <Link to="/benches" className="muted" style={{ fontSize: 13 }}>← Back to benches & sites</Link>
      {showDelete && <DeleteModal siteId={id!} domain={site.domain} onClose={() => setShowDelete(false)} onDone={() => nav("/benches")} />}
    </>
  );
}

function DeleteModal({ siteId, domain, onClose, onDone }: { siteId: string; domain: string; onClose: () => void; onDone: () => void }) {
  const qc = useQueryClient();
  const [user, setUser] = useState("root");
  const [pw, setPw] = useState("");
  const mut = useMutation({
    mutationFn: () => apiPost(`/sites/${siteId}/delete`, { db_root_user: user, db_root_password: pw }),
    onSuccess: () => {
      toast.success("Site deletion queued", { description: domain });
      qc.invalidateQueries({ queryKey: ["sites"] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onDone();
    },
    onError: (e: Error) => toast.error("Could not delete site", { description: e.message }),
  });
  return (
    <Modal title={`Delete ${domain}?`} description="Drops the site database and removes it from nginx. This cannot be undone." onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
        <div className="modal-body">
          <label className="field">DB root user<input type="text" value={user} onChange={(e) => setUser(e.target.value)} required /></label>
          <label className="field">DB root password<input type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoFocus required /></label>
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button variant="danger" type="submit" loading={mut.isPending}>Confirm delete</Button>
        </div>
      </form>
    </Modal>
  );
}
