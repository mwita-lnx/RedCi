import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Fleet, Heartbeat } from "../api";
import { Empty, PageHeader, PanelBox } from "../ui";

function ago(unix: number): string {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  return `${Math.floor(s / 3600)}h ago`;
}

export function FleetServerDetailPage() {
  const { id } = useParams();

  // Fleet list gives us the server's name/online status (polls every 10s).
  const { data: fleet } = useQuery({
    queryKey: ["fleet"],
    queryFn: () => apiGet<Fleet>("/servers/fleet"),
    refetchInterval: 10_000,
  });
  const server = fleet?.servers.find((s) => String(s.id) === id);

  // Heartbeats poll fast so the view feels live (agent beats every ~15s).
  const { data, isLoading } = useQuery({
    queryKey: ["heartbeats", id],
    queryFn: () => apiGet<{ heartbeats: Heartbeat[] }>(`/servers/${id}/heartbeats`),
    refetchInterval: 2_000,
  });
  const beats = data?.heartbeats ?? [];
  const latest = beats[0];

  return (
    <>
      <PageHeader
        title={server?.name ?? `Server ${id}`}
        sub={
          server
            ? `${server.online ? "online" : "offline"} · agent ${server.agent_version || "—"} · beats every ~15s`
            : "live heartbeat feed"
        }
        action={<Link to="/fleet" className="muted" style={{ fontSize: 13 }}>← Back to fleet</Link>}
      />

      <PanelBox title="Latest heartbeat">
        {!latest ? (
          <Empty>No heartbeats received yet. Is the agent online?</Empty>
        ) : (
          <div style={{ padding: 14 }}>
            <div className="muted" style={{ fontSize: 12.5, marginBottom: 8 }}>
              received {ago(latest.at)} · running job {latest.running_job_id || "none"}
            </div>
            <pre className="logbox loglines" style={{ margin: 0 }}>
              {JSON.stringify(latest, null, 2)}
            </pre>
          </div>
        )}
      </PanelBox>

      <PanelBox title={`Recent heartbeats${beats.length ? ` (${beats.length})` : ""}`}>
        {isLoading ? (
          <div className="skeleton" style={{ height: 120 }} />
        ) : beats.length === 0 ? (
          <Empty>Nothing yet.</Empty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Received</th>
                <th>Agent</th>
                <th>Running job</th>
                <th>CPU</th>
                <th>Mem</th>
                <th>Disk</th>
              </tr>
            </thead>
            <tbody>
              {beats.map((b, i) => {
                const f = b.facts as Record<string, number>;
                return (
                  <tr key={`${b.at}-${i}`}>
                    <td className="mono">{ago(b.at)}</td>
                    <td className="mono">{b.agent_version || "—"}</td>
                    <td className="mono">{b.running_job_id || "—"}</td>
                    <td className="mono">{f?.cpu_used_pct != null ? `${f.cpu_used_pct}%` : "—"}</td>
                    <td className="mono">{f?.mem_used_pct != null ? `${f.mem_used_pct}%` : "—"}</td>
                    <td className="mono">{f?.disk_used_pct != null ? `${f.disk_used_pct}%` : "—"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </PanelBox>
    </>
  );
}
