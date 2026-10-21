import { Fragment, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiDelete, apiGet, apiPost, Bench, Grant, Site, User } from "../api";
import { Badge, Button, Empty, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function UsersPage({ embedded }: { embedded?: boolean } = {}) {
  const { data, isLoading } = useQuery({ queryKey: ["users"], queryFn: () => apiGet<User[]>("/users") });
  const [openUser, setOpenUser] = useState<number | null>(null);
  return (
    <>
      {!embedded && <PageHeader title="Users" sub="Panel operators, their global role, and per-site access" />}
      <PanelBox>
        {isLoading ? <TableSkeleton cols={4} /> : !data?.length ? <Empty>No users.</Empty> : (
          <table>
            <thead><tr><th>Email</th><th>Global role</th><th>Status</th><th>Site access</th></tr></thead>
            <tbody>
              {data.map((u) => (
                <Fragment key={u.id}>
                  <tr>
                    <td style={{ fontWeight: 550 }}>{u.email}</td>
                    <td><Badge status={u.role} /></td>
                    <td><Badge status={u.disabled ? "disabled" : "active"} /></td>
                    <td>
                      <Button variant="ghost" size="sm" onClick={() => setOpenUser(openUser === u.id ? null : u.id)}>
                        {openUser === u.id ? "Hide" : "Manage"}
                      </Button>
                    </td>
                  </tr>
                  {openUser === u.id && (
                    <tr>
                      <td colSpan={4} style={{ background: "var(--panel-2,#0f1420)" }}>
                        <GrantsEditor user={u} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
    </>
  );
}

// GrantsEditor lists a user's per-resource grants and lets an admin add/remove
// them. A grant raises the user's effective role on that site/bench above their
// global role (it never lowers it).
function GrantsEditor({ user }: { user: User }) {
  const qc = useQueryClient();
  const key = ["grants", user.id];
  const { data: grants } = useQuery({ queryKey: key, queryFn: () => apiGet<Grant[]>(`/users/${user.id}/grants`) });
  const { data: sites } = useQuery({ queryKey: ["sites"], queryFn: () => apiGet<Site[]>("/sites") });
  const { data: benches } = useQuery({ queryKey: ["benches"], queryFn: () => apiGet<Bench[]>("/benches") });

  const [kind, setKind] = useState<"site" | "bench">("site");
  const [resourceId, setResourceId] = useState<number>(0);
  const [role, setRole] = useState("deployer");

  const invalidate = () => qc.invalidateQueries({ queryKey: key });
  const add = useMutation({
    mutationFn: () => apiPost(`/users/${user.id}/grants`, { resource_type: kind, resource_id: resourceId, role }),
    onSuccess: () => { toast.success("Grant added"); setResourceId(0); invalidate(); },
    onError: (e: Error) => toast.error("Could not add grant", { description: e.message }),
  });
  const remove = useMutation({
    mutationFn: (gid: number) => apiDelete(`/users/${user.id}/grants/${gid}`),
    onSuccess: () => { toast.success("Grant removed"); invalidate(); },
    onError: (e: Error) => toast.error("Could not remove grant", { description: e.message }),
  });

  const label = (g: Grant) => {
    if (g.resource_type === "site") {
      const s = (sites ?? []).find((x) => x.id === g.resource_id);
      return s ? s.domain : `site #${g.resource_id}`;
    }
    const b = (benches ?? []).find((x) => x.id === g.resource_id);
    return b ? `${b.name} (bench)` : `bench #${g.resource_id}`;
  };

  return (
    <div style={{ padding: "10px 6px", display: "flex", flexDirection: "column", gap: 10 }}>
      <div className="muted" style={{ fontSize: 12 }}>
        Grants raise <b>{user.email}</b>'s role on specific resources above their global <b>{user.role}</b>. A bench grant covers all its sites.
      </div>
      {!grants?.length ? (
        <div className="muted" style={{ fontSize: 12.5 }}>No grants — effective role everywhere is the global <b>{user.role}</b>.</div>
      ) : (
        <table style={{ fontSize: 12.5 }}>
          <thead><tr><th>Resource</th><th>Type</th><th>Role</th><th /></tr></thead>
          <tbody>
            {grants.map((g) => (
              <tr key={g.id}>
                <td className="mono">{label(g)}</td>
                <td>{g.resource_type}</td>
                <td><Badge status={g.role} /></td>
                <td><Button variant="ghost" size="sm" loading={remove.isPending && remove.variables === g.id} onClick={() => remove.mutate(g.id)}>Revoke</Button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div style={{ display: "flex", gap: 8, alignItems: "flex-end", flexWrap: "wrap" }}>
        <label className="field" style={{ margin: 0 }}>Type
          <select value={kind} onChange={(e) => { setKind(e.target.value as "site" | "bench"); setResourceId(0); }}>
            <option value="site">site</option><option value="bench">bench</option>
          </select>
        </label>
        <label className="field" style={{ margin: 0, minWidth: 220 }}>Resource
          <select value={resourceId} onChange={(e) => setResourceId(Number(e.target.value))}>
            <option value={0} disabled>Select a {kind}…</option>
            {kind === "site"
              ? (sites ?? []).map((s) => <option key={s.id} value={s.id}>{s.domain}</option>)
              : (benches ?? []).map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        </label>
        <label className="field" style={{ margin: 0 }}>Role
          <select value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="viewer">viewer</option><option value="deployer">deployer</option><option value="admin">admin</option>
          </select>
        </label>
        <Button size="sm" disabled={!resourceId} loading={add.isPending} onClick={() => add.mutate()}>Add grant</Button>
      </div>
    </div>
  );
}
