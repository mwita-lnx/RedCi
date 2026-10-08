import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, Bench } from "../api";
import { Button, Modal } from "../ui";

// Shared "New site" modal, used from the Benches & sites page. An optional
// preselected bench is used when opening it from a specific bench.
export function NewSiteModal({ onClose, benchId }: { onClose: () => void; benchId?: number }) {
  const qc = useQueryClient();
  const { data: benches } = useQuery({ queryKey: ["benches"], queryFn: () => apiGet<Bench[]>("/benches") });
  const [form, setForm] = useState({
    bench_id: benchId ?? 0, domain: "", apps: "", admin_password: "",
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
      qc.invalidateQueries({ queryKey: ["benches-overview"] });
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
