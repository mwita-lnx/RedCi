import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, Route, RouteLocation, Server } from "../api";
import { Badge, Button, Empty, Modal, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function RoutesPage({ role }: { role?: string }) {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["routes"], queryFn: () => apiGet<Route[]>("/routes") });
  const canAdmin = atLeast(role ?? "", "admin");
  const [showNew, setShowNew] = useState(false);
  const [detailId, setDetailId] = useState<number | null>(null);
  const apply = useMutation({
    mutationFn: (id: number) => apiPost(`/routes/${id}/apply`),
    onSuccess: () => { toast.success("Route apply queued"); qc.invalidateQueries({ queryKey: ["deploys"] }); },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <>
      <PageHeader
        title="Routes & nginx"
        sub="Proxy routes the agent manages under /etc/nginx/conf.d"
        action={canAdmin && <Button onClick={() => setShowNew(true)}>+ New route</Button>}
      />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={5} /> : !data?.length ? <Empty>No routes yet.</Empty> : (
          <table>
            <thead><tr><th>Domain</th><th>Type</th><th>Target</th><th>SSL</th><th>Status</th><th></th></tr></thead>
            <tbody>
              {data.map((rt) => (
                <tr key={rt.id} onClick={() => setDetailId(rt.id)} style={{ cursor: "pointer" }}>
                  <td style={{ fontWeight: 550 }}>
                    <a onClick={(e) => { e.stopPropagation(); setDetailId(rt.id); }} style={{ cursor: "pointer" }}>{rt.domain}</a>
                  </td>
                  <td>{rt.kind === "frontend-split"
                    ? <span className="badge" style={{ background: "var(--info-bg,#1f2a44)" }}>frontend-split</span>
                    : <span className="muted">proxy</span>}</td>
                  <td className="mono">
                    {rt.kind === "frontend-split"
                      ? `${rt.locations?.length ?? 0} location${rt.locations?.length === 1 ? "" : "s"}${rt.frappe_site ? ` · frappe:${rt.frappe_site}` : ""}`
                      : rt.upstream}
                  </td>
                  <td className="muted">{rt.ssl ? "on" : "off"}</td>
                  <td><Badge status={rt.status} /></td>
                  <td className="row-actions" onClick={(e) => e.stopPropagation()}>
                    <Button variant="ghost" size="sm" onClick={() => setDetailId(rt.id)}>View</Button>
                    {canAdmin && <Button variant="secondary" size="sm" loading={apply.isPending && apply.variables === rt.id} onClick={() => apply.mutate(rt.id)}>Apply</Button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
      {showNew && <NewRouteModal onClose={() => setShowNew(false)} onDone={() => { setShowNew(false); qc.invalidateQueries({ queryKey: ["routes"] }); }} />}
      {detailId !== null && <RouteDetailModal id={detailId} canAdmin={canAdmin} onClose={() => setDetailId(null)} onApply={(id) => apply.mutate(id)} />}
    </>
  );
}

function RouteDetailModal({ id, canAdmin, onClose, onApply }: { id: number; canAdmin: boolean; onClose: () => void; onApply: (id: number) => void }) {
  const { data: rt, isLoading } = useQuery({ queryKey: ["route", id], queryFn: () => apiGet<Route>(`/routes/${id}`) });
  return (
    <Modal title={rt?.domain ?? "Route"} description={rt ? `${rt.kind === "frontend-split" ? "Frappe + frontend split" : "Simple proxy"} · ${rt.status}` : "Loading…"} onClose={onClose} wide>
      <div className="modal-body">
        {isLoading || !rt ? <div className="skeleton" style={{ height: 160 }} /> : (
          <>
            <div className="kv">
              <div><div className="k">Domain</div><div className="v mono" style={{ fontSize: 14 }}>{rt.domain}</div></div>
              {!!rt.aliases?.length && <div><div className="k">Aliases</div><div className="v mono" style={{ fontSize: 14 }}>{rt.aliases.join(", ")}</div></div>}
              <div><div className="k">SSL</div><div className="v" style={{ fontSize: 14 }}>{rt.ssl ? "on" : "off"}</div></div>
              <div><div className="k">Status</div><div className="v"><Badge status={rt.status} /></div></div>
              {rt.frappe_site && <div><div className="k">Frappe site</div><div className="v mono" style={{ fontSize: 14 }}>{rt.frappe_site}</div></div>}
              {rt.static_root && <div><div className="k">Static root</div><div className="v mono" style={{ fontSize: 14 }}>{rt.static_root}</div></div>}
              {rt.config_hash && <div><div className="k">Config hash</div><div className="v mono" style={{ fontSize: 14 }}>{rt.config_hash.slice(0, 12)}</div></div>}
            </div>

            {rt.kind === "frontend-split" ? (
              <>
                <div className="muted" style={{ fontSize: 12.5, margin: "10px 0 4px" }}>Location rules (first match wins)</div>
                <table style={{ fontSize: 12.5 }}>
                  <thead><tr><th>Path</th><th>Match</th><th>Action</th><th>Target</th><th>WS</th><th>Frappe</th></tr></thead>
                  <tbody>
                    {(rt.locations ?? []).map((l, i) => (
                      <tr key={i}>
                        <td className="mono">{l.path}</td>
                        <td>{l.match ?? "prefix"}</td>
                        <td>{l.action}</td>
                        <td className="mono">{l.action === "filesystem" ? (l.target || rt.static_root || "(static root)") : l.target}</td>
                        <td style={{ textAlign: "center" }}>{l.websocket ? "✓" : ""}</td>
                        <td style={{ textAlign: "center" }}>{l.frappe_proxy ? "✓" : ""}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            ) : (
              <div className="kv"><div><div className="k">Upstream</div><div className="v mono" style={{ fontSize: 14 }}>{rt.upstream}</div></div></div>
            )}

            {rt.snippet && (
              <>
                <div className="muted" style={{ fontSize: 12.5, margin: "10px 0 4px" }}>Extra nginx snippet</div>
                <pre className="logbox loglines" style={{ margin: 0 }}>{rt.snippet}</pre>
              </>
            )}

            {rt.config && (
              <>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", margin: "12px 0 4px" }}>
                  <span className="muted" style={{ fontSize: 12.5 }}>Rendered nginx config · /etc/nginx/conf.d/00-{rt.domain}.conf</span>
                  <Button type="button" variant="ghost" size="sm" onClick={() => { navigator.clipboard?.writeText(rt.config!); toast.success("Config copied"); }}>Copy</Button>
                </div>
                <pre className="logbox loglines" style={{ margin: 0, maxHeight: "40vh", overflow: "auto" }}>{rt.config}</pre>
                <div className="muted" style={{ fontSize: 11, marginTop: 4 }}>Preview assumes nginx 1.25+ (http2 on). The agent re-renders with the host's detected version on apply.</div>
              </>
            )}
          </>
        )}
      </div>
      <div className="modal-foot">
        <Button variant="ghost" type="button" onClick={onClose}>Close</Button>
        {canAdmin && rt && <Button onClick={() => { onApply(rt.id); onClose(); }}>Apply</Button>}
      </div>
    </Modal>
  );
}

const BLANK_LOC: RouteLocation = { path: "", match: "prefix", action: "proxy", target: "", websocket: false, frappe_proxy: false };

// The standard Frappe + Next.js split, mirroring the KRCS NGINX report §5.
function presetFrontendSplit(frappeUpstream: string, nextUpstream: string, socketUpstream: string): RouteLocation[] {
  const f = (path: string, match: RouteLocation["match"] = "prefix"): RouteLocation =>
    ({ path, match, action: "proxy", target: frappeUpstream, frappe_proxy: true, websocket: false });
  return [
    f("/api/"), f("/files/"), f("/private/"), f("/app"), f("/method/"),
    f("/website_script.js", "exact"),
    { path: "/assets", match: "prefix", action: "filesystem", target: "", websocket: false, frappe_proxy: false },
    { path: "/socket.io/", match: "prefix", action: "proxy", target: socketUpstream, frappe_proxy: true, websocket: true },
    { path: "/", match: "prefix", action: "proxy", target: nextUpstream, frappe_proxy: false, websocket: false },
  ];
}

function NewRouteModal({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { data: servers } = useQuery({ queryKey: ["servers"], queryFn: () => apiGet<Server[]>("/servers") });
  const [serverId, setServerId] = useState<number>(0);
  const [domain, setDomain] = useState("");
  const [ssl, setSsl] = useState(true);
  const [mode, setMode] = useState<"proxy" | "frontend-split">("proxy");
  const [upstream, setUpstream] = useState("http://127.0.0.1:3000");
  const [frappeSite, setFrappeSite] = useState("");
  const [staticRoot, setStaticRoot] = useState("");
  const [aliases, setAliases] = useState("");
  const [locations, setLocations] = useState<RouteLocation[]>([{ ...BLANK_LOC }]);

  const create = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = {
        server_id: serverId, domain: domain.trim(), ssl,
      };
      if (mode === "proxy") {
        body.upstream = upstream.trim();
      } else {
        body.frappe_site = frappeSite.trim();
        body.static_root = staticRoot.trim();
        body.aliases = aliases.split(",").map((a) => a.trim()).filter(Boolean);
        body.locations = locations.filter((l) => l.path.trim());
      }
      return apiPost("/routes", body);
    },
    onSuccess: () => { toast.success("Route created — click Apply to issue SSL + reload nginx"); onDone(); },
    onError: (e: Error) => toast.error("Could not create route", { description: e.message }),
  });

  const setLoc = (i: number, patch: Partial<RouteLocation>) =>
    setLocations((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  return (
    <Modal title="New route" description="A simple proxy, or a Frappe + frontend path-split vhost." onClose={onClose} wide>
      <form onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
        <div className="modal-body">
          <label className="field">Server
            <select value={serverId} onChange={(e) => setServerId(Number(e.target.value))} required>
              <option value={0} disabled>Select a server…</option>
              {(servers ?? []).map((s) => <option key={s.id} value={s.id}>{s.name} ({s.status})</option>)}
            </select>
          </label>
          <label className="field">Domain<input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="app.example.com" required /></label>
          <label className="field" style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
            <input type="checkbox" checked={ssl} onChange={(e) => setSsl(e.target.checked)} /> Issue SSL (Let's Encrypt) on apply
          </label>

          <div className="rtabs" style={{ marginTop: 8 }}>
            <button type="button" className={mode === "proxy" ? "on" : ""} onClick={() => setMode("proxy")}>Simple proxy</button>
            <button type="button" className={mode === "frontend-split" ? "on" : ""} onClick={() => setMode("frontend-split")}>Frappe + frontend split</button>
          </div>

          {mode === "proxy" ? (
            <label className="field">Upstream<input className="mono" value={upstream} onChange={(e) => setUpstream(e.target.value)} placeholder="http://127.0.0.1:3000" required /></label>
          ) : (
            <>
              <div style={{ display: "flex", gap: 10 }}>
                <label className="field" style={{ flex: 1 }}>Frappe site (X-Frappe-Site-Name)<input value={frappeSite} onChange={(e) => setFrappeSite(e.target.value)} placeholder="app.example.com" /></label>
                <label className="field" style={{ flex: 1 }}>Static root (/assets)<input className="mono" value={staticRoot} onChange={(e) => setStaticRoot(e.target.value)} placeholder="/home/frappe/frappe-bench/sites" /></label>
              </div>
              <label className="field">Aliases (comma-separated, optional)<input value={aliases} onChange={(e) => setAliases(e.target.value)} placeholder="www.example.com, example.com" /></label>

              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 6 }}>
                <span className="muted" style={{ fontSize: 12.5 }}>Location rules (first match wins; order matters)</span>
                <Button type="button" variant="ghost" size="sm" onClick={() => setLocations(presetFrontendSplit("http://127.0.0.1:8000", "http://127.0.0.1:3000", "http://127.0.0.1:9000"))}>
                  Load Frappe+Next.js preset
                </Button>
              </div>

              <table style={{ fontSize: 12.5 }}>
                <thead><tr><th>Path</th><th>Match</th><th>Action</th><th>Target</th><th>WS</th><th>Frappe</th><th></th></tr></thead>
                <tbody>
                  {locations.map((l, i) => (
                    <tr key={i}>
                      <td><input className="mono" style={{ width: 120 }} value={l.path} onChange={(e) => setLoc(i, { path: e.target.value })} placeholder="/api/" /></td>
                      <td>
                        <select value={l.match} onChange={(e) => setLoc(i, { match: e.target.value as RouteLocation["match"] })}>
                          <option value="prefix">prefix</option><option value="exact">exact</option><option value="regex">regex</option>
                        </select>
                      </td>
                      <td>
                        <select value={l.action} onChange={(e) => setLoc(i, { action: e.target.value as RouteLocation["action"] })}>
                          <option value="proxy">proxy</option><option value="filesystem">filesystem</option><option value="redirect">redirect</option>
                        </select>
                      </td>
                      <td><input className="mono" style={{ width: 150 }} value={l.target} onChange={(e) => setLoc(i, { target: e.target.value })} placeholder={l.action === "filesystem" ? "(static root)" : l.action === "redirect" ? "https://…" : "http://127.0.0.1:8000"} /></td>
                      <td style={{ textAlign: "center" }}><input type="checkbox" disabled={l.action !== "proxy"} checked={!!l.websocket} onChange={(e) => setLoc(i, { websocket: e.target.checked })} /></td>
                      <td style={{ textAlign: "center" }}><input type="checkbox" disabled={l.action !== "proxy"} checked={!!l.frappe_proxy} onChange={(e) => setLoc(i, { frappe_proxy: e.target.checked })} /></td>
                      <td><Button type="button" variant="ghost" size="sm" onClick={() => setLocations((ls) => ls.filter((_, j) => j !== i))}>✕</Button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <Button type="button" variant="secondary" size="sm" onClick={() => setLocations((ls) => [...ls, { ...BLANK_LOC }])}>+ Add location</Button>
            </>
          )}
        </div>
        <div className="modal-foot">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={create.isPending} disabled={!serverId || !domain.trim()}>Create route</Button>
        </div>
      </form>
    </Modal>
  );
}
