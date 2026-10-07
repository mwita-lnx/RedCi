import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, Bench, Site } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function SitesPage({ role }: { role?: string }) {
  const { data: sites, isLoading } = useQuery({ queryKey: ["sites"], queryFn: () => apiGet<Site[]>("/sites") });
  const [showNew, setShowNew] = useState(false);
  const canAdmin = atLeast(role ?? "", "admin");

  return (
    <>
      <PageHeader
        title="Sites"
        sub="Frappe sites managed by the panel"
        action={canAdmin && <Button onClick={() => setShowNew(true)}>New site</Button>}
      />
      <PanelBox>
        {isLoading ? (
          <TableSkeleton cols={4} />
        ) : !sites?.length ? (
          <Empty>No sites yet.</Empty>
        ) : (
          <table>
            <thead><tr><th>Domain</th><th>Status</th><th>SSL</th><th></th></tr></thead>
            <tbody>
              {sites.map((s) => (
                <tr key={s.id}>
                  <td><Link to={`/sites/${s.id}`}>{s.domain}</Link></td>
                  <td><Badge status={s.status} /></td>
                  <td className="muted">{s.ssl ? "on" : "off"}</td>
                  <td className="row-actions">
                    <Link className="btn secondary sm" to={`/sites/${s.id}`}>View</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
      {showNew && <NewSiteModal onClose={() => setShowNew(false)} />}
    </>
  );
}

function NewSiteModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const { data: benches } = useQuery({ queryKey: ["benches"], queryFn: () => apiGet<Bench[]>("/benches") });
  const [form, setForm] = useState({
    bench_id: 0, domain: "", apps: "", admin_password: "",
    db_root_user: "root", db_root_password: "", le_email: "", ssl: false,
  });
  const mut = useMutation({
    mutationFn: () => apiPost("/sites", {
      ...form,
      bench_id: Number(form.bench_id),
      apps: form.apps.split(/[\s,]+/).filter(Boolean),
    }),
    onSuccess: () => {
      toast.success("Site creation queued", { description: form.domain });
      qc.invalidateQueries({ queryKey: ["sites"] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onClose();
    },
    onError: (e: Error) => toast.error("Could not create site", { description: e.message }),
  });
  const set = (k: string, v: unknown) => setForm((f) => ({ ...f, [k]: v }));

  return (
    <Modal title="New site" description="Create a Frappe site with apps and optional SSL" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
        <div className="modal-body">
          {!benches?.length ? (
            <p className="muted">No benches yet. Add a server, refresh and import its benches first.</p>
          ) : (
            <>
              <label className="field">Bench
                <select value={form.bench_id} onChange={(e) => set("bench_id", e.target.value)} required>
                  <option value="">Select a bench…</option>
                  {benches.map((b) => <option key={b.id} value={b.id}>{b.path}</option>)}
                </select>
              </label>
              <label className="field">Domain
                <input type="text" value={form.domain} onChange={(e) => set("domain", e.target.value)} placeholder="client1.example.com" required />
              </label>
              <label className="field">Apps (comma or space separated)
                <input type="text" value={form.apps} onChange={(e) => set("apps", e.target.value)} placeholder="erpnext hrms" required />
              </label>
              <label className="field">Admin password
                <input type="password" value={form.admin_password} onChange={(e) => set("admin_password", e.target.value)} required />
              </label>
              <label className="field">DB root user
                <input type="text" value={form.db_root_user} onChange={(e) => set("db_root_user", e.target.value)} required />
              </label>
              <label className="field">DB root password
                <input type="password" value={form.db_root_password} onChange={(e) => set("db_root_password", e.target.value)} required />
              </label>
              <label className="field">Let's Encrypt email (for SSL)
                <input type="email" value={form.le_email} onChange={(e) => set("le_email", e.target.value)} placeholder="ops@example.com" />
              </label>
              <label className="check"><input type="checkbox" checked={form.ssl} onChange={(e) => set("ssl", e.target.checked)} /> Enable SSL</label>
            </>
          )}
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={mut.isPending} disabled={!benches?.length}>Create site</Button>
        </div>
      </form>
    </Modal>
  );
}
