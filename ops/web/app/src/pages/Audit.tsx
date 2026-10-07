import { useQuery } from "@tanstack/react-query";
import { apiGet, Audit } from "../api";
import { Empty, PageHeader, PanelBox, TableSkeleton } from "../ui";

function when(ts: number) { return new Date(ts * 1000).toLocaleString(); }

export function AuditPage() {
  const { data, isLoading } = useQuery({ queryKey: ["audit"], queryFn: () => apiGet<Audit[]>("/audit") });
  return (
    <>
      <PageHeader title="Audit log" sub="Every action, most recent first" />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={4} /> : !data?.length ? <Empty>No audit entries.</Empty> : (
          <table>
            <thead><tr><th>When</th><th>Actor</th><th>Action</th><th>Target</th></tr></thead>
            <tbody>
              {data.map((e) => (
                <tr key={e.id}>
                  <td className="muted">{when(e.created_at)}</td>
                  <td>{e.actor || "system"}</td>
                  <td style={{ fontWeight: 550 }}>{e.action}</td>
                  <td className="muted">{e.object_type}{e.object_id ? `:${e.object_id}` : ""} {e.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
    </>
  );
}
