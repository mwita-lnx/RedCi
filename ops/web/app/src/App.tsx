import { NavLink, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Me } from "./api";
import { DashboardPage } from "./pages/Dashboard";
import { SitesPage } from "./pages/Sites";
import { SiteDetailPage } from "./pages/SiteDetail";
import { DeploysPage } from "./pages/Deploys";
import { JobPage } from "./pages/Job";
import { ServersPage } from "./pages/Servers";
import { AppSourcesPage } from "./pages/AppSources";
import { WebAppsPage } from "./pages/WebApps";
import { RoutesPage } from "./pages/RoutesPage";
import { UsersPage } from "./pages/Users";
import { AuditPage } from "./pages/Audit";
import { PipelinesPage } from "./pages/Pipelines";
import { PipelineDetailPage } from "./pages/PipelineDetail";
import { BenchesSitesPage } from "./pages/BenchesSites";

const NAV = [
  { to: "/", label: "Overview", end: true },
  { to: "/pipelines", label: "Pipelines" },
  { to: "/deploys", label: "Runs" },
  { to: "/benches", label: "Benches & sites" },
  { to: "/sites", label: "Sites" },
  { to: "/web-apps", label: "Web apps" },
  { to: "/app-sources", label: "App sources" },
  { to: "/routes", label: "Routes & nginx" },
  { to: "/servers", label: "Servers" },
  { to: "/users", label: "Users" },
  { to: "/audit", label: "Audit" },
];

export function App() {
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: () => apiGet<Me>("/me") });
  const loc = useLocation();
  return (
    <div className="shell">
      <nav className="sidebar">
        <div className="brand">
          <span className="logo">R</span>
          <span className="name">redci</span>
        </div>
        <div className="workspace">
          <span className="ws-avatar">{(me?.email[0] ?? "R").toUpperCase()}</span>
          <span className="ws-meta">
            <b>Workspace</b>
            <span>on-prem fleet</span>
          </span>
        </div>
        <div className="nav">
          {NAV.map((n) => (
            <NavLink key={n.to} to={n.to} end={n.end}>
              {n.label}
            </NavLink>
          ))}
        </div>
        <div className="sidebar-footer">
          {me && (
            <div className="user-chip">
              <span className="avatar">{me.email[0]?.toUpperCase()}</span>
              <span className="meta">
                <span>{me.email}</span>
                <span className="role">{me.role}</span>
              </span>
            </div>
          )}
          <form method="post" action="/logout">
            <button className="logout-btn" style={{ width: "100%" }} type="submit">Sign out</button>
          </form>
        </div>
      </nav>
      <main className="main page-enter" key={loc.pathname}>
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/pipelines" element={<PipelinesPage role={me?.role} />} />
          <Route path="/pipelines/:id" element={<PipelineDetailPage role={me?.role} />} />
          <Route path="/benches" element={<BenchesSitesPage role={me?.role} />} />
          <Route path="/sites" element={<SitesPage role={me?.role} />} />
          <Route path="/sites/:id" element={<SiteDetailPage role={me?.role} />} />
          <Route path="/deploys" element={<DeploysPage />} />
          <Route path="/jobs/:id" element={<JobPage />} />
          <Route path="/servers" element={<ServersPage role={me?.role} />} />
          <Route path="/app-sources" element={<AppSourcesPage role={me?.role} />} />
          <Route path="/web-apps" element={<WebAppsPage />} />
          <Route path="/routes" element={<RoutesPage role={me?.role} />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="/audit" element={<AuditPage />} />
        </Routes>
      </main>
    </div>
  );
}
