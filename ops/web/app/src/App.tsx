import { NavLink, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Me } from "./api";
import { toggleTheme, useTheme } from "./theme";
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
import { FleetPage } from "./pages/Fleet";
import { useState } from "react";

// Primary nav mirrors the product mockups.
const NAV = [
  { to: "/", label: "Overview", end: true },
  { to: "/deploys", label: "Runs" },
  { to: "/benches", label: "Benches & sites" },
  { to: "/app-sources", label: "Apps" },
  { to: "/fleet", label: "Servers & agents" },
  { to: "/pipelines", label: "Deployments" },
];
// Secondary items live in the overflow menu.
const OVERFLOW = [
  { to: "/sites", label: "Sites" },
  { to: "/web-apps", label: "Web apps" },
  { to: "/routes", label: "Routes & nginx" },
  { to: "/servers", label: "Servers (legacy)" },
  { to: "/users", label: "Users" },
  { to: "/audit", label: "Audit" },
];

export function App() {
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: () => apiGet<Me>("/me") });
  const theme = useTheme();
  const loc = useLocation();
  return (
    <div className="shell">
      <nav className="navbar">
        <div className="brand">
          <span className="logo">R</span>
          <span className="name">redci</span>
        </div>
        <div className="nav">
          {NAV.map((n) => (
            <NavLink key={n.to} to={n.to} end={n.end}>{n.label}</NavLink>
          ))}
        </div>
        <div className="nav-right">
          <OverflowMenu />
          <button className="theme-btn" type="button" onClick={toggleTheme}
            title={theme === "dark" ? "Switch to light" : "Switch to dark"}>
            {theme === "dark" ? "☀" : "☾"}
          </button>
          <form method="post" action="/logout"><button className="theme-btn" type="submit">Sign out</button></form>
          <span className="nav-avatar" title={me?.email}>{(me?.email[0] ?? "R").toUpperCase()}</span>
        </div>
      </nav>
      <main className="main page-enter" key={loc.pathname}>
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/pipelines" element={<PipelinesPage role={me?.role} />} />
          <Route path="/pipelines/:id" element={<PipelineDetailPage role={me?.role} />} />
          <Route path="/benches" element={<BenchesSitesPage role={me?.role} />} />
          <Route path="/fleet" element={<FleetPage role={me?.role} />} />
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

function OverflowMenu() {
  const [open, setOpen] = useState(false);
  return (
    <div className="nav-overflow" onMouseLeave={() => setOpen(false)}>
      <button className="of-btn" onClick={() => setOpen((o) => !o)}>More ▾</button>
      {open && (
        <div className="of-menu">
          {OVERFLOW.map((n) => (
            <NavLink key={n.to} to={n.to} onClick={() => setOpen(false)}>{n.label}</NavLink>
          ))}
        </div>
      )}
    </div>
  );
}
