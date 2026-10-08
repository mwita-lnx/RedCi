import { useState } from "react";
import { atLeast } from "../api";
import { PageHeader, PanelBox } from "../ui";
import { UsersPage } from "./Users";
import { AuditPage } from "./Audit";
import { Documentation } from "./Documentation";

type Tab = "docs" | "users" | "audit";

export function SettingsPage({ role }: { role?: string }) {
  const isAdmin = atLeast(role ?? "", "admin");
  const [tab, setTab] = useState<Tab>("docs");
  const tabs: { id: Tab; label: string; show: boolean }[] = [
    { id: "docs", label: "Documentation", show: true },
    { id: "users", label: "Users", show: isAdmin },
    { id: "audit", label: "Audit log", show: true },
  ];

  return (
    <>
      <PageHeader title="Settings" sub="Documentation, users and audit" />
      <div className="settings-tabs">
        {tabs.filter((t) => t.show).map((t) => (
          <button key={t.id} className={tab === t.id ? "on" : ""} onClick={() => setTab(t.id)}>{t.label}</button>
        ))}
      </div>

      {tab === "docs" && <PanelBox><div style={{ padding: "20px 22px" }}><Documentation /></div></PanelBox>}
      {tab === "users" && isAdmin && <UsersPage embedded />}
      {tab === "audit" && <AuditPage embedded />}
    </>
  );
}
