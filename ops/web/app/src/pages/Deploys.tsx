import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Deploy } from "../api";
import { Badge, Empty, PageHeader, PanelBox, TableSkeleton } from "../ui";

function ago(ts: number): string {
  const s = Math.floor(Date.now() / 1000 - ts);
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export function DeploysPage() {
  // Poll while anything is in flight so progress shows without a manual refresh.
  const { data, isLoading } = useQuery({
    queryKey: ["deploys"], queryFn: () => apiGet<Deploy[]>("/deploys"),
    refetchInterval: (q) => {
      const rows = q.state.data as Deploy[] | undefined;
      return rows?.some((d) => ["queued", "running", "ready", "claimed"].includes(d.status)) ? 2000 : 8000;
    },
  });
  return (
    <>
      <PageHeader title="Deploys" sub="Every operation, most recent first" />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={6} /> : !data?.length ? <Empty>No deploys yet.</Empty> : (
          <table>
            <thead><tr><th>#</th><th>Kind</th><th>Target</th><th>Trigger</th><th>Status</th><th>When</th><th></th></tr></thead>
            <tbody>
              {data.map((d) => (
                <tr key={d.id}>
                  <td className="muted">{d.id}</td>
                  <td style={{ fontWeight: 550 }}>{d.kind}</td>
                  <td className="muted">{d.target_type}:{d.target_id}</td>
                  <td className="muted">{d.trigger}</td>
                  <td>
                    <Badge status={d.status} />
                    {d.error && <div style={{ color: "var(--brand)", fontSize: 12, marginTop: 4, maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{d.error}</div>}
                  </td>
                  <td className="muted">{ago(d.created_at)}</td>
                  <td className="row-actions">
                    {d.last_job_id > 0 && <Link className="btn secondary sm" to={`/jobs/${d.last_job_id}`}>Logs</Link>}
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
