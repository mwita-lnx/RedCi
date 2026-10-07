import { useQuery } from "@tanstack/react-query";
import { apiGet, WebApp } from "../api";
import { Badge, Empty, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function WebAppsPage() {
  const { data, isLoading } = useQuery({ queryKey: ["web-apps"], queryFn: () => apiGet<WebApp[]>("/web-apps"), refetchInterval: 15_000 });
  return (
    <>
      <PageHeader title="Web apps" sub="Next.js / Docker apps" />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={4} /> : !data?.length ? <Empty>No web apps yet.</Empty> : (
          <table>
            <thead><tr><th>Name</th><th>Port</th><th>Status</th><th>Commit</th></tr></thead>
            <tbody>
              {data.map((a) => (
                <tr key={a.id}>
                  <td style={{ fontWeight: 550 }}>{a.name}</td>
                  <td className="mono">127.0.0.1:{a.internal_port}</td>
                  <td><Badge status={a.status} /></td>
                  <td className="mono">{a.commit ? a.commit.slice(0, 7) : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
    </>
  );
}
