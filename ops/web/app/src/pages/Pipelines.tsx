import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, AppSource, Bench, Pipeline, Server, Site } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox, TableSkeleton } from "../ui";

function shortSha(c: string) { return c ? c.slice(0, 8) : "—"; }
function prodEnv(p: Pipeline) { return p.envs[p.envs.length - 1]; }

export function PipelinesPage({ role }: { role?: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["pipelines"], queryFn: () => apiGet<Pipeline[]>("/pipelines"), refetchInterval: 8000,
  });
  const [showNew, setShowNew] = useState(false);
  const canAdmin = atLeast(role ?? "", "admin");

  return (
    <>
      <PageHeader
        title="Pipelines"
        sub="Release trains — promote an app from dev to staging to production"
        action={canAdmin && <Button onClick={() => setShowNew(true)}>New pipeline</Button>}
      />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={5} /> : !data?.length ? (
          <Empty>No pipelines yet. Create one to wire dev → staging → prod.</Empty>
        ) : (
          <table>
            <thead><tr><th>Pipeline</th><th>App</th><th>Environments</th><th>Production</th><th></th></tr></thead>
            <tbody>
              {data.map((p) => {
                const prod = prodEnv(p);
                return (
                  <tr key={p.id}>
                    <td><Link to={`/pipelines/${p.id}`}>{p.name}</Link></td>
                    <td className="mono">{p.app}</td>
                    <td className="muted">{p.envs.map((e) => e.name).join(" → ")}</td>
                    <td>
                      <span className="mono">{shortSha(prod?.current_commit)}</span>
                      {prod && <span style={{ marginLeft: 8 }}><Badge status={prod.status} /></span>}
                    </td>
                    <td className="row-actions">
                      <Link className="btn secondary sm" to={`/pipelines/${p.id}`}>Open</Link>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </PanelBox>
      {showNew && <NewPipelineModal onClose={() => setShowNew(false)} />}
    </>
  );
}

interface EnvDraft { name: string; server_id: number; site_id: number; require_approval: boolean }

function NewPipelineModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const { data: sources } = useQuery({ queryKey: ["app-sources"], queryFn: () => apiGet<AppSource[]>("/app-sources") });
  const { data: servers } = useQuery({ queryKey: ["servers"], queryFn: () => apiGet<Server[]>("/servers") });
  const { data: sites } = useQuery({ queryKey: ["sites"], queryFn: () => apiGet<Site[]>("/sites") });
  const { data: benches } = useQuery({ queryKey: ["benches"], queryFn: () => apiGet<Bench[]>("/benches") });

  const [name, setName] = useState("");
  const [appSourceId, setAppSourceId] = useState<number>(0);
  const [envs, setEnvs] = useState<EnvDraft[]>([
    { name: "staging", server_id: 0, site_id: 0, require_approval: false },
    { name: "production", server_id: 0, site_id: 0, require_approval: true },
  ]);

  // map site -> server via bench, to filter sites by chosen agent.
  const siteServer = useMemo(() => {
    const benchServer = new Map((benches ?? []).map((b) => [b.id, b.server_id]));
    const m = new Map<number, number>();
    for (const s of sites ?? []) m.set(s.id, benchServer.get(s.bench_id) ?? 0);
    return m;
  }, [benches, sites]);

  const sitesForServer = (serverId: number) =>
    (sites ?? []).filter((s) => !serverId || siteServer.get(s.id) === serverId);

  const setEnv = (i: number, patch: Partial<EnvDraft>) =>
    setEnvs((prev) => prev.map((e, j) => (j === i ? { ...e, ...patch } : e)));
  const addEnv = () => setEnvs((p) => [...p, { name: "", server_id: 0, site_id: 0, require_approval: true }]);
  const removeEnv = (i: number) => setEnvs((p) => p.filter((_, j) => j !== i));

  const mut = useMutation({
    mutationFn: () => apiPost("/pipelines", {
      name, app_source_id: Number(appSourceId),
      envs: envs.map((e) => ({ name: e.name, site_id: Number(e.site_id), require_approval: e.require_approval })),
    }),
    onSuccess: () => {
      toast.success("Pipeline created", { description: name });
      qc.invalidateQueries({ queryKey: ["pipelines"] });
      onClose();
    },
    onError: (e: Error) => toast.error("Could not create pipeline", { description: e.message }),
  });

  const valid = name && appSourceId && envs.length >= 2 && envs.every((e) => e.name && e.site_id);

  return (
    <Modal title="New pipeline" description="Pick an app and map each environment to a site on a specific agent" onClose={onClose}>
      <div className="modal-body">
        <label className="field">Name<input type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder="api-gateway" /></label>
        <label className="field">App source
          <select value={appSourceId} onChange={(e) => setAppSourceId(Number(e.target.value))}>
            <option value={0}>Select an app…</option>
            {(sources ?? []).filter((s) => s.kind === "frappe").map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
          </select>
        </label>

        <div style={{ fontSize: 12, color: "var(--text-muted)", fontWeight: 600, marginTop: 4 }}>Environments (in order)</div>
        {envs.map((env, i) => (
          <div key={i} style={{ border: "1px solid var(--border)", borderRadius: 8, padding: 12, display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <input type="text" value={env.name} onChange={(e) => setEnv(i, { name: e.target.value })} placeholder="env name (staging)" style={{ flex: 1 }} />
              {envs.length > 2 && <Button variant="ghost" size="sm" onClick={() => removeEnv(i)}>Remove</Button>}
            </div>
            <label className="field">Agent / server
              <select value={env.server_id} onChange={(e) => setEnv(i, { server_id: Number(e.target.value), site_id: 0 })}>
                <option value={0}>Any agent</option>
                {(servers ?? []).map((s) => <option key={s.id} value={s.id}>{s.name} ({s.status})</option>)}
              </select>
            </label>
            <label className="field">Site
              <select value={env.site_id} onChange={(e) => setEnv(i, { site_id: Number(e.target.value) })}>
                <option value={0}>Select a site…</option>
                {sitesForServer(env.server_id).map((s) => <option key={s.id} value={s.id}>{s.domain}</option>)}
              </select>
            </label>
            <label className="check"><input type="checkbox" checked={env.require_approval} onChange={(e) => setEnv(i, { require_approval: e.target.checked })} /> Require approval to promote into this env</label>
          </div>
        ))}
        <Button variant="secondary" size="sm" onClick={addEnv}>+ Add environment</Button>
      </div>
      <div className="modal-foot">
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button loading={mut.isPending} disabled={!valid} onClick={() => mut.mutate()}>Create pipeline</Button>
      </div>
    </Modal>
  );
}
