import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, Route } from "../api";
import { Badge, Button, Empty, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function RoutesPage({ role }: { role?: string }) {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["routes"], queryFn: () => apiGet<Route[]>("/routes") });
  const canAdmin = atLeast(role ?? "", "admin");
  const apply = useMutation({
    mutationFn: (id: number) => apiPost(`/routes/${id}/apply`),
    onSuccess: () => { toast.success("Route apply queued"); qc.invalidateQueries({ queryKey: ["deploys"] }); },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <>
      <PageHeader title="Routes & nginx" sub="Proxy routes the agent manages under /etc/nginx/ops.d" />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={5} /> : !data?.length ? <Empty>No routes yet.</Empty> : (
          <table>
            <thead><tr><th>Domain</th><th>Upstream</th><th>SSL</th><th>Status</th><th></th></tr></thead>
            <tbody>
              {data.map((rt) => (
                <tr key={rt.id}>
                  <td style={{ fontWeight: 550 }}>{rt.domain}</td>
                  <td className="mono">{rt.upstream}</td>
                  <td className="muted">{rt.ssl ? "on" : "off"}</td>
                  <td><Badge status={rt.status} /></td>
                  <td className="row-actions">
                    {canAdmin && <Button variant="secondary" size="sm" loading={apply.isPending && apply.variables === rt.id} onClick={() => apply.mutate(rt.id)}>Apply</Button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
    </>
  );
}
