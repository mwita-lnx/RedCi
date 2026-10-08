import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiGet, apiPost, atLeast, GithubStatus } from "../api";
import { Button, Modal, PageHeader, PanelBox } from "../ui";
import { UsersPage } from "./Users";
import { AuditPage } from "./Audit";
import { Documentation } from "./Documentation";

type Tab = "docs" | "integrations" | "users" | "audit";

export function SettingsPage({ role }: { role?: string }) {
  const isAdmin = atLeast(role ?? "", "admin");
  const [tab, setTab] = useState<Tab>("docs");
  const tabs: { id: Tab; label: string; show: boolean }[] = [
    { id: "docs", label: "Documentation", show: true },
    { id: "integrations", label: "Integrations", show: isAdmin },
    { id: "users", label: "Users", show: isAdmin },
    { id: "audit", label: "Audit log", show: true },
  ];

  return (
    <>
      <PageHeader title="Settings" sub="Documentation, integrations, users and audit" />
      <div className="settings-tabs">
        {tabs.filter((t) => t.show).map((t) => (
          <button key={t.id} className={tab === t.id ? "on" : ""} onClick={() => setTab(t.id)}>{t.label}</button>
        ))}
      </div>

      {tab === "docs" && <PanelBox><div style={{ padding: "20px 22px" }}><Documentation /></div></PanelBox>}
      {tab === "integrations" && isAdmin && <IntegrationsTab />}
      {tab === "users" && isAdmin && <UsersPage embedded />}
      {tab === "audit" && <AuditPage embedded />}
    </>
  );
}

function IntegrationsTab() {
  const { data: gh } = useQuery({ queryKey: ["gh-status"], queryFn: () => apiGet<GithubStatus>("/github/status") });
  return (
    <div className="stack">
      <GitHubCard gh={gh} />
    </div>
  );
}

export function GitHubCard({ gh }: { gh?: GithubStatus }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [token, setToken] = useState("");
  const mut = useMutation({
    mutationFn: () => apiPost<GithubStatus>("/github/token", { token }),
    onSuccess: (r) => {
      toast.success(`Connected to GitHub as ${r.login}`);
      qc.invalidateQueries({ queryKey: ["gh-status"] });
      qc.invalidateQueries({ queryKey: ["app-sources"] });
      qc.invalidateQueries({ queryKey: ["bench"] });
      setOpen(false); setToken("");
    },
    onError: (e: Error) => toast.error("GitHub connection failed", { description: e.message }),
  });
  return (
    <PanelBox title="GitHub">
      <div className="gh-card">
        <div className="gh-left">
          <span className="gh-ic"></span>
          <div>
            <div className="gh-title">GitHub</div>
            <div className="gh-sub">
              {gh?.connected
                ? <><span className="gh-dot on" />Connected as <b className="on">{gh.login}</b> — agents use this token to clone private repos</>
                : <><span className="gh-dot off" />Not connected — add a Personal Access Token to browse repos and track versions</>}
            </div>
          </div>
        </div>
        <Button variant={gh?.connected ? "secondary" : "primary"} onClick={() => setOpen(true)}>
          {gh?.connected ? "Reconnect" : "Connect GitHub"}
        </Button>
      </div>
      {open && (
        <Modal title="Connect GitHub" description="Paste a Personal Access Token. Stored encrypted; also used by agents to clone private repos." onClose={() => setOpen(false)}>
          <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }}>
            <div className="modal-body">
              <label className="field">Personal Access Token
                <input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="ghp_… or github_pat_…" className="mono" autoFocus required />
              </label>
              <div className="muted" style={{ fontSize: 12 }}>
                Scopes: <b>repo</b> for private repos (classic) or <b>Contents: read</b> (fine-grained).
              </div>
            </div>
            <div className="modal-foot">
              <Button variant="ghost" type="button" onClick={() => setOpen(false)}>Cancel</Button>
              <Button type="submit" loading={mut.isPending}>Save &amp; verify</Button>
            </div>
          </form>
        </Modal>
      )}
    </PanelBox>
  );
}
