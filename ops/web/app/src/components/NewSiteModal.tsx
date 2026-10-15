import { useState, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { apiGet, apiPost, AppSource, Bench, BenchDetail, GhRepo, GithubStatus } from "../api";
import { Button, Modal } from "../ui";

// ── Official Frappe app catalog ───────────────────────────────────────────────
const OFFICIAL: { name: string; label: string; desc: string; repo: string }[] = [
  { name: "erpnext",       label: "ERPNext",         desc: "Full-suite ERP: accounting, inventory, HR, CRM",  repo: "frappe/erpnext" },
  { name: "hrms",          label: "Frappe HRMS",     desc: "Human resources & payroll management",            repo: "frappe/hrms" },
  { name: "payments",      label: "Payments",        desc: "Payment gateways & reconciliation",               repo: "frappe/payments" },
  { name: "lms",           label: "Frappe LMS",      desc: "Learning management system",                      repo: "frappe/lms" },
  { name: "crm",           label: "Frappe CRM",      desc: "Sales CRM pipeline & contacts",                   repo: "frappe/crm" },
  { name: "helpdesk",      label: "Frappe Helpdesk", desc: "Customer support & ticketing",                    repo: "frappe/helpdesk" },
  { name: "wiki",          label: "Frappe Wiki",     desc: "Internal knowledge base",                         repo: "frappe/wiki" },
  { name: "webshop",       label: "Webshop",         desc: "E-commerce storefront for ERPNext",               repo: "frappe/webshop" },
  { name: "lending",       label: "Lending",         desc: "Loan management & disbursement",                  repo: "frappe/lending" },
  { name: "healthcare",    label: "Healthcare",      desc: "Hospital & clinic management",                    repo: "frappe/health" },
  { name: "education",     label: "Education",       desc: "School & university management",                  repo: "frappe/education" },
  { name: "manufacturing", label: "Manufacturing",   desc: "Shop floor, BOM & work orders",                   repo: "frappe/manufacturing" },
  { name: "agriculture",   label: "Agriculture",     desc: "Farm & crop management",                          repo: "frappe/agriculture" },
  { name: "non_profit",    label: "Non Profit",      desc: "Membership & grant management",                   repo: "frappe/non_profit" },
];

function short(sha: string) { return sha ? sha.slice(0, 7) : ""; }
function initials(s: string) { return s.slice(0, 2).toUpperCase(); }

type AppEntry = { name: string; label: string; desc: string; tag: "official" | "source" | "repo" };

// ── Bench-aware app picker ────────────────────────────────────────────────────
function AppPicker({ selected, onChange, benchDetail, ghConnected, sources, repos }: {
  selected: string[];
  onChange: (apps: string[]) => void;
  benchDetail: BenchDetail | undefined;
  ghConnected: boolean;
  sources: AppSource[];
  repos: GhRepo[];
}) {
  const [tab, setTab] = useState<"official" | "yours">("official");
  const [q, setQ] = useState("");

  // Map of appName → bench app info for quick lookup
  const onBench = useMemo(() => {
    const m = new Map<string, BenchDetail["apps"][0]>();
    for (const a of benchDetail?.apps ?? []) m.set(a.name, a);
    return m;
  }, [benchDetail]);

  const toggle = (name: string) => {
    onChange(selected.includes(name) ? selected.filter((a) => a !== name) : [...selected, name]);
  };

  const allYours: AppEntry[] = useMemo(() => {
    const out: AppEntry[] = sources.map((s) => ({
      name: s.name, label: s.name, desc: `${s.repo} · ${s.branch}`, tag: "source" as const,
    }));
    for (const r of repos) {
      const rname = r.full_name.split("/")[1];
      if (!out.find((e) => e.name === rname))
        out.push({ name: rname, label: rname, desc: r.full_name + (r.private ? " (private)" : ""), tag: "repo" as const });
    }
    return out;
  }, [sources, repos]);

  const filtered = (tab === "official"
    ? OFFICIAL.map((a) => ({ ...a, tag: "official" as const }))
    : allYours
  ).filter((a) => !q
    || a.name.toLowerCase().includes(q.toLowerCase())
    || a.label.toLowerCase().includes(q.toLowerCase())
    || a.desc.toLowerCase().includes(q.toLowerCase())
  );

  const conflicts = selected.filter((name) => {
    const b = onBench.get(name);
    return b && b.latest_commit && b.current_commit && b.latest_commit !== b.current_commit;
  });

  return (
    <div className="app-picker">
      {/* Selected chips */}
      {selected.length > 0 && (
        <div className="ap-chips">
          {selected.map((name) => {
            const b = onBench.get(name);
            const behind = b && b.latest_commit && b.current_commit && b.latest_commit !== b.current_commit;
            return (
              <span key={name} className={`ap-chip ${behind ? "warn" : ""}`} title={behind ? `Bench is at ${short(b!.current_commit)}, latest is ${short(b!.latest_commit)}` : undefined}>
                {name}{b ? ` ${short(b.current_commit)}` : ""}
                {behind && <span className="ap-chip-warn">⚠</span>}
                <button type="button" className="ap-chip-rm" onClick={() => toggle(name)}>×</button>
              </span>
            );
          })}
        </div>
      )}

      {/* Conflict notice */}
      {conflicts.length > 0 && (
        <div className="ap-conflict-banner">
          <b>⚠ Version behind on {conflicts.length} app{conflicts.length > 1 ? "s" : ""}:</b>{" "}
          {conflicts.map((name) => {
            const b = onBench.get(name)!;
            return <span key={name}><b>{name}</b> bench={short(b.current_commit)} latest={short(b.latest_commit)}</span>;
          }).reduce<React.ReactNode[]>((acc, el, i) => i === 0 ? [el] : [...acc, ", ", el], [])}
          . The site will be created with the bench's current version.
        </div>
      )}

      {/* Tab bar + search */}
      <div className="ap-bar">
        <div className="ap-tabs">
          <button type="button" className={tab === "official" ? "on" : ""} onClick={() => setTab("official")}>Official apps</button>
          <button type="button" className={tab === "yours" ? "on" : ""} onClick={() => setTab("yours")}>
            Your repos{!ghConnected && <span className="ap-warn"> · no GitHub</span>}
          </button>
        </div>
        <input className="ap-search" placeholder="Search…" value={q} onChange={(e) => setQ(e.target.value)} />
      </div>

      {/* List */}
      <div className="ap-list">
        {filtered.length === 0 && (
          <div className="ap-empty">
            {tab === "yours" && !ghConnected
              ? "Connect GitHub in Settings → Integrations to see your repos."
              : "No apps match."}
          </div>
        )}
        {filtered.map((a) => {
          const on = selected.includes(a.name);
          const b = onBench.get(a.name);
          const installed = !!b;
          const behind = installed && b.latest_commit && b.current_commit && b.latest_commit !== b.current_commit;
          const uptodate = installed && !behind;

          return (
            <div key={a.name} className={`ap-row ${on ? "on" : ""}`} onClick={() => toggle(a.name)}>
              <span className="ap-ic">{initials(a.label)}</span>
              <div className="ap-info">
                <span className="ap-name">{a.label}</span>
                <span className="ap-desc">{a.desc}</span>
                {installed && (
                  <span className={`ap-bench-state ${behind ? "warn" : "ok"}`}>
                    {behind
                      ? `⚠ bench: ${short(b.current_commit)} · latest: ${short(b.latest_commit)}`
                      : `✓ on bench: ${short(b.current_commit) || b.branch || "installed"}`}
                  </span>
                )}
              </div>
              <span className="ap-check">{on ? "✓" : ""}</span>
              <div className="ap-badges">
                {a.tag === "official" && <span className="ap-badge official">official</span>}
                {a.tag === "source" && <span className="ap-badge source">source</span>}
                {uptodate && <span className="ap-badge installed">installed</span>}
                {behind && <span className="ap-badge outdated">outdated</span>}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── Step indicator ────────────────────────────────────────────────────────────
type Step = "bench" | "apps" | "creds";
const STEPS: { id: Step; label: string }[] = [
  { id: "bench", label: "Bench" },
  { id: "apps",  label: "Apps" },
  { id: "creds", label: "Credentials" },
];

function StepBar({ current }: { current: Step }) {
  const idx = STEPS.findIndex((s) => s.id === current);
  return (
    <div className="ns-steps">
      {STEPS.map((s, i) => {
        const done = i < idx;
        const on = i === idx;
        return (
          <>
            <span key={s.id} className={`ns-step ${on ? "on" : done ? "done" : ""}`}>
              <span className="ns-step-n">{done ? "✓" : i + 1}</span> {s.label}
            </span>
            {i < STEPS.length - 1 && <span key={`line-${i}`} className="ns-step-line" />}
          </>
        );
      })}
    </div>
  );
}

// ── Modal ─────────────────────────────────────────────────────────────────────
export function NewSiteModal({ onClose, benchId }: { onClose: () => void; benchId?: number }) {
  const qc = useQueryClient();
  const nav = useNavigate();
  const [step, setStep] = useState<Step>(benchId !== undefined ? "apps" : "bench");

  const benchFixed = benchId !== undefined;

  const { data: benches } = useQuery({
    queryKey: ["benches"], queryFn: () => apiGet<Bench[]>("/benches"),
  });
  const [form, setForm] = useState({
    bench_id: benchId ?? 0, domain: "",
    admin_password: "", db_root_user: "root", db_root_password: "",
    le_email: "", ssl: false,
  });
  const set = (k: string, v: unknown) => setForm((f) => ({ ...f, [k]: v }));

  const activeBenchId = Number(form.bench_id) || benchId;

  // Fetch bench detail once a bench is selected — drives conflict display
  const { data: benchDetail, isFetching: loadingBench } = useQuery({
    queryKey: ["bench", String(activeBenchId)],
    queryFn: () => apiGet<BenchDetail>(`/benches/${activeBenchId}`),
    enabled: !!activeBenchId,
  });

  const { data: gh } = useQuery({ queryKey: ["gh-status"], queryFn: () => apiGet<GithubStatus>("/github/status") });
  const { data: sources = [] } = useQuery({ queryKey: ["app-sources"], queryFn: () => apiGet<AppSource[]>("/app-sources") });
  const { data: repos = [] } = useQuery({
    queryKey: ["gh-repos"], queryFn: () => apiGet<GhRepo[]>("/github/repos"), enabled: !!gh?.connected,
  });

  const [selectedApps, setSelectedApps] = useState<string[]>([]);

  const mut = useMutation({
    mutationFn: () => apiPost<{ deploy_id: number }>("/sites", {
      ...form, bench_id: Number(form.bench_id), apps: selectedApps,
    }),
    onSuccess: (r) => {
      toast.success("Site creation queued", { description: form.domain });
      qc.invalidateQueries({ queryKey: ["benches-overview"] });
      qc.invalidateQueries({ queryKey: ["bench", String(activeBenchId)] });
      qc.invalidateQueries({ queryKey: ["deploys"] });
      onClose();
      if (r.deploy_id) nav("/deploys");
    },
    onError: (e: Error) => toast.error("Could not create site", { description: e.message }),
  });

  const selectedBench = benches?.find((b) => b.id === Number(form.bench_id));
  const canGoApps = benchFixed ? true : Number(form.bench_id) > 0;
  const canGoCreds = !!form.domain && selectedApps.length > 0;
  const canSubmit = canGoCreds && !!form.admin_password && !!form.db_root_password;

  const descMap: Record<Step, string> = {
    bench: "Pick the bench to create this site on",
    apps:  "Select apps to install — conflicts with bench versions are highlighted",
    creds: "Set site credentials and SSL",
  };

  return (
    <Modal title="New site" description={descMap[step]} onClose={onClose} wide>
      <StepBar current={step} />

      <form onSubmit={(e) => {
        e.preventDefault();
        if (step === "bench") setStep("apps");
        else if (step === "apps") setStep("creds");
        else mut.mutate();
      }}>
        <div className="modal-body">

          {/* ── Step 1: Bench ── */}
          {step === "bench" && (
            <>
              {!benches?.length ? (
                <p className="muted">No benches yet. Add a server, refresh and import its benches first.</p>
              ) : (
                <div className="bench-select-list">
                  {benches.map((b) => (
                    <div
                      key={b.id}
                      className={`bsl-row ${Number(form.bench_id) === b.id ? "on" : ""}`}
                      onClick={() => set("bench_id", b.id)}
                    >
                      <span className="bsl-check">{Number(form.bench_id) === b.id ? "●" : "○"}</span>
                      <div className="bsl-info">
                        <span className="bsl-name">{b.name || b.path.split("/").pop()}</span>
                        <span className="bsl-path">{b.path}</span>
                      </div>
                      {loadingBench && Number(form.bench_id) === b.id && (
                        <span className="muted" style={{ fontSize: 12 }}>loading…</span>
                      )}
                      {benchDetail && Number(form.bench_id) === b.id && (
                        <span className="bsl-meta">{benchDetail.apps.length} app{benchDetail.apps.length !== 1 ? "s" : ""} · {benchDetail.sites.length} site{benchDetail.sites.length !== 1 ? "s" : ""}</span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </>
          )}

          {/* ── Step 2: Apps ── */}
          {step === "apps" && (
            <>
              {!benchFixed && selectedBench && (
                <div className="ns-recap">
                  <span className="ns-recap-domain" style={{ fontSize: 13 }}>
                    Bench: <b>{selectedBench.path}</b>
                    {benchDetail && <span className="muted"> · {benchDetail.apps.length} apps installed</span>}
                  </span>
                </div>
              )}
              <label className="field">Domain
                <input type="text" value={form.domain} onChange={(e) => set("domain", e.target.value)}
                  placeholder="client.example.com" autoFocus required />
              </label>
              <div className="field-label">Apps to install</div>
              <AppPicker
                selected={selectedApps}
                onChange={setSelectedApps}
                benchDetail={benchDetail}
                ghConnected={!!gh?.connected}
                sources={sources}
                repos={repos}
              />
            </>
          )}

          {/* ── Step 3: Credentials ── */}
          {step === "creds" && (
            <>
              <div className="ns-recap">
                <span className="ns-recap-domain">{form.domain}</span>
                <span className="ns-recap-apps">{selectedApps.join(", ")}</span>
              </div>
              <label className="field">Admin password
                <input type="password" value={form.admin_password} onChange={(e) => set("admin_password", e.target.value)} autoFocus required />
              </label>
              <label className="field">DB root user
                <input type="text" value={form.db_root_user} onChange={(e) => set("db_root_user", e.target.value)} required />
              </label>
              <label className="field">DB root password
                <input type="password" value={form.db_root_password} onChange={(e) => set("db_root_password", e.target.value)} required />
              </label>
              <label className="field">Let's Encrypt email <span className="muted">(optional, for SSL)</span>
                <input type="email" value={form.le_email} onChange={(e) => set("le_email", e.target.value)} placeholder="ops@example.com" />
              </label>
              <label className="check">
                <input type="checkbox" checked={form.ssl} onChange={(e) => set("ssl", e.target.checked)} /> Enable SSL
              </label>
            </>
          )}
        </div>

        <div className="modal-foot">
          {step === "bench" && (
            <><Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
              <Button type="submit" disabled={!canGoApps}>Next →</Button></>
          )}
          {step === "apps" && (
            <>{!benchFixed && <Button variant="ghost" type="button" onClick={() => setStep("bench")}>← Back</Button>}
              {benchFixed && <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>}
              <Button type="submit" disabled={!canGoCreds}>Next →</Button></>
          )}
          {step === "creds" && (
            <><Button variant="ghost" type="button" onClick={() => setStep("apps")}>← Back</Button>
              <Button type="submit" loading={mut.isPending} disabled={!canSubmit}>Create site</Button></>
          )}
        </div>
      </form>
    </Modal>
  );
}
