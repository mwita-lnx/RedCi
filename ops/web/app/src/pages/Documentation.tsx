import { useState } from "react";

const SECTIONS = [
  { id: "start", label: "Getting started" },
  { id: "agents", label: "Agents & benches" },
  { id: "sites", label: "Sites & apps" },
  { id: "github", label: "GitHub integration" },
  { id: "pipelines", label: "Pipelines & promotion" },
  { id: "runs", label: "Runs & logs" },
  { id: "rollback", label: "Backups & rollback" },
  { id: "fleet", label: "Fleet & health" },
];

export function Documentation() {
  const [active, setActive] = useState("start");
  const go = (id: string) => {
    setActive(id);
    document.getElementById(`doc-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  };
  return (
    <div className="docs">
      <nav className="docs-toc">
        {SECTIONS.map((s) => (
          <a key={s.id} className={active === s.id ? "on" : ""} onClick={() => go(s.id)}>{s.label}</a>
        ))}
      </nav>
      <div className="docs-body">
        <section id="doc-start" className="docs-section">
          <h2>Getting started</h2>
          <p>RedCi is an on-prem control plane for Frappe benches and Next.js/Docker web apps. The <b>panel</b> (this UI) coordinates work; lightweight <b>agents</b> run on your servers and execute jobs. Nothing deploys without an agent claiming the job.</p>
          <p>The typical flow:</p>
          <div className="step"><span className="n">1</span><span>Add a server and install its agent (Servers &amp; agents).</span></div>
          <div className="step"><span className="n">2</span><span>Refresh &amp; import the server's benches and sites.</span></div>
          <div className="step"><span className="n">3</span><span>Connect GitHub and register your app sources (Apps).</span></div>
          <div className="step"><span className="n">4</span><span>Build a pipeline mapping dev → staging → prod sites, then promote releases.</span></div>
        </section>

        <section id="doc-agents" className="docs-section">
          <h2>Agents &amp; benches</h2>
          <p>An <b>agent</b> is a single Go binary that registers with the panel, heartbeats every 15s (reporting live CPU / memory / disk), and long-polls for jobs. It needs no inbound ports — it dials out to the panel.</p>
          <h3>Enrolling a server</h3>
          <p>On <b>Servers &amp; agents</b>, add a server to get a one-time enrollment command. Run it on the host:</p>
          <pre><code>ops-agent register --panel https://your-panel --token &lt;token&gt;
ops-agent run</code></pre>
          <p>A <b>bench host</b> is any server with one or more Frappe benches; a <b>runner</b> has none. After enrollment, use <b>Refresh</b> then <b>Import</b> to pull the server's benches, sites, and app versions into the panel.</p>
          <div className="note">The agent reads bench facts from files (Procfile, <code>common_site_config.json</code>, <code>pyvenv.cfg</code>) — no changes are made to your bench during discovery.</div>
        </section>

        <section id="doc-sites" className="docs-section">
          <h2>Sites &amp; apps</h2>
          <p>Open <b>Benches &amp; sites</b> to see every bench as a collapsible card. Expand a bench to list its sites; click a site to see its installed apps, versions, backups, and actions.</p>
          <h3>Creating a site</h3>
          <p>Use <b>+ New site</b> (globally, or on a specific bench). You pick the bench, domain, apps, admin + DB-root credentials, and optional SSL. The panel runs <code>bench new-site</code>, installs the apps, sets up nginx, and (if requested) issues a Let's Encrypt certificate — each as a tracked job.</p>
        </section>

        <section id="doc-github" className="docs-section">
          <h2>GitHub integration</h2>
          <p>On <b>Apps</b>, connect GitHub with a Personal Access Token (scope <code>repo</code> for private repos, or fine-grained <i>Contents: read</i>). The token is stored encrypted and is also used by agents to clone private repos.</p>
          <p>Once connected you can:</p>
          <ul>
            <li>Pick a repo &amp; branch from dropdowns when adding an app source.</li>
            <li>See each app source's latest commit vs. what's deployed.</li>
            <li>Choose a specific commit from a list when promoting a pipeline.</li>
          </ul>
        </section>

        <section id="doc-pipelines" className="docs-section">
          <h2>Pipelines &amp; promotion</h2>
          <p>A <b>pipeline</b> (under <b>Deployments</b>) is a release train for one app source across ordered environment nodes — typically <code>dev → staging → production</code>. <b>Each node binds a bench and a site</b>, so different environments can live on different agents/machines, even one site or many.</p>
          <h3>Setting one up</h3>
          <div className="step"><span className="n">1</span><span>Choose the app source the train ships.</span></div>
          <div className="step"><span className="n">2</span><span>For each node, pick the <b>agent → bench → site</b> it targets and whether it needs approval.</span></div>
          <div className="step"><span className="n">3</span><span>Save. The pipeline detail page shows each node as a card with its current version, health, and the agent it runs on.</span></div>
          <h3>Promoting a change</h3>
          <p>Promotion deploys the chosen commit of the app onto the next node's site, on that node's agent. If the node requires approval it waits for a deployer/admin to <b>Approve &amp; ship</b>. Each node advances independently; the <b>change timeline</b> on the pipeline page shows where every release is across the train.</p>
          <div className="note">Promotion ships <b>code only</b> — each environment keeps its own database. Use a site's Backup/rollback for data.</div>
        </section>

        <section id="doc-runs" className="docs-section">
          <h2>Runs &amp; logs</h2>
          <p><b>Runs</b> lists every operation (deploys, migrations, backups, promotions), grouped by day with live status. Open a run to see its pipeline of steps, live streaming output, and — for Frappe migrations — the individual patches as they execute.</p>
          <p>Runs stream over Server-Sent Events, so logs appear in real time while a job is running and remain viewable afterwards.</p>
        </section>

        <section id="doc-rollback" className="docs-section">
          <h2>Backups &amp; rollback</h2>
          <p>Every Frappe app deploy takes a <code>bench backup</code> of each affected site <i>before</i> migrating, and records the previous commit. If a change goes wrong, open the site and <b>Roll back</b>: the panel checks out the previous commit and restores the pre-deploy database.</p>
          <div className="note">Rollback returns code <b>and</b> data to the last known-good point — data written since that deploy is lost. That's the safe trade-off for a broken migration.</div>
        </section>

        <section id="doc-fleet" className="docs-section">
          <h2>Fleet &amp; health</h2>
          <p><b>Servers &amp; agents</b> shows the whole fleet: per-server CPU / memory / disk (sampled live each heartbeat), role, agent version, and uptime. The <b>Needs attention</b> panel flags offline agents, servers low on disk, and outdated agent versions automatically.</p>
          <p>A server goes <b>offline</b> if the panel hasn't heard a heartbeat within the threshold; its in-flight jobs' leases expire and are surfaced for review.</p>
        </section>
      </div>
    </div>
  );
}
