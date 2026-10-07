import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, Server } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function ServersPage({ role }: { role?: string }) {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["servers"], queryFn: () => apiGet<Server[]>("/servers"), refetchInterval: 15_000 });
  const [showAdd, setShowAdd] = useState(false);
  const canAdmin = atLeast(role ?? "", "admin");

  const refresh = useMutation({
    mutationFn: (sid: number) => apiPost(`/servers/${sid}/refresh`),
    onSuccess: () => { toast.success("Refresh queued"); qc.invalidateQueries({ queryKey: ["deploys"] }); },
    onError: (e: Error) => toast.error(e.message),
  });
  const imported = useMutation({
    mutationFn: (sid: number) => apiPost<{ benches: number; sites: number }>(`/servers/${sid}/import`),
    onSuccess: (r) => { toast.success(`Imported ${r.benches} benches, ${r.sites} sites`); qc.invalidateQueries({ queryKey: ["sites"] }); },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <>
      <PageHeader title="Servers" sub="On-prem servers and their agents" action={canAdmin && <Button onClick={() => setShowAdd(true)}>Add server</Button>} />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={5} /> : !data?.length ? <Empty>No servers yet.</Empty> : (
          <table>
            <thead><tr><th>Name</th><th>Hostname</th><th>Status</th><th>Agent</th><th></th></tr></thead>
            <tbody>
              {data.map((sv) => (
                <tr key={sv.id}>
                  <td style={{ fontWeight: 550 }}>{sv.name}</td>
                  <td className="muted">{sv.hostname}</td>
                  <td><Badge status={sv.status} /></td>
                  <td className="mono">{sv.agent_version || "—"}</td>
                  <td className="row-actions">
                    {canAdmin && <>
                      <Button variant="secondary" size="sm" loading={refresh.isPending && refresh.variables === sv.id} onClick={() => refresh.mutate(sv.id)}>Refresh</Button>
                      <Button variant="secondary" size="sm" loading={imported.isPending && imported.variables === sv.id} onClick={() => imported.mutate(sv.id)}>Import</Button>
                    </>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
      {showAdd && <AddServerModal onClose={() => setShowAdd(false)} />}
    </>
  );
}

function AddServerModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState({ name: "", hostname: "" });
  const [cmd, setCmd] = useState("");
  const mut = useMutation({
    mutationFn: () => apiPost<{ enroll_cmd: string }>("/servers", form),
    onSuccess: (r) => { setCmd(r.enroll_cmd); qc.invalidateQueries({ queryKey: ["servers"] }); toast.success("Enrollment token created"); },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Modal title="Add server" description="Create a one-time enrollment token" onClose={onClose}>
      {cmd ? (
        <>
          <div className="modal-body">
            <p className="muted">Run this on the server within 1 hour (shown once):</p>
            <pre className="mono" style={{ background: "var(--page-bg)", padding: 12, borderRadius: 9, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>{cmd}</pre>
          </div>
          <div className="modal-foot"><Button onClick={onClose}>Done</Button></div>
        </>
      ) : (
        <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
          <div className="modal-body">
            <label className="field">Name<input type="text" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="srv1" required /></label>
            <label className="field">Hostname<input type="text" value={form.hostname} onChange={(e) => setForm({ ...form, hostname: e.target.value })} placeholder="srv1.internal" /></label>
          </div>
          <div className="modal-foot">
            <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
            <Button type="submit" loading={mut.isPending}>Create token</Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
