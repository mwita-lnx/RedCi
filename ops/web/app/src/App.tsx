import { NavLink, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, Me } from "./api";
import { toggleTheme, useTheme } from "./theme";
import { DashboardPage } from "./pages/Dashboard";
import { SiteDetailPage } from "./pages/SiteDetail";
import { DeploysPage } from "./pages/Deploys";
import { JobPage } from "./pages/Job";
import { ServersPage } from "./pages/Servers";
import { WebAppsPage } from "./pages/WebApps";
import { BenchDetailPage } from "./pages/BenchDetail";
import { RoutesPage } from "./pages/RoutesPage";
import { PipelinesPage } from "./pages/Pipelines";
import { PipelineDetailPage } from "./pages/PipelineDetail";
import { BenchesSitesPage } from "./pages/BenchesSites";
import { FleetPage } from "./pages/Fleet";
import { SettingsPage } from "./pages/Settings";

// Primary nav mirrors the product mockups.
const NAV = [
  { to: "/", label: "Overview", end: true },
  { to: "/deploys", label: "Runs" },
  { to: "/benches", label: "Benches & sites" },
  { to: "/web-apps", label: "Web apps" },
  { to: "/fleet", label: "Servers & agents" },
  { to: "/pipelines", label: "Deployments" },
  { to: "/routes", label: "Routes & nginx" },
  { to: "/settings", label: "Settings" },
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
          <Route path="/sites/:id" element={<SiteDetailPage role={me?.role} />} />
          <Route path="/deploys" element={<DeploysPage />} />
          <Route path="/jobs/:id" element={<JobPage />} />
          <Route path="/servers" element={<ServersPage role={me?.role} />} />
          <Route path="/benches/:id" element={<BenchDetailPage role={me?.role} />} />
          <Route path="/web-apps" element={<WebAppsPage />} />
          <Route path="/routes" element={<RoutesPage role={me?.role} />} />
          <Route path="/settings" element={<SettingsPage role={me?.role} />} />
        </Routes>
      </main>
    </div>
  );
}
