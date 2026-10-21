package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// CreateSiteDeploy stores the site's secrets, creates the site row, and builds
// a deploy: new_site, then issue_certificate when SSL is requested. Returns the
// deploy id.
func (s *Server) CreateSiteDeploy(ctx context.Context, benchID int64, domain string, apps []string, adminPassword, dbRootUser, dbRootPassword, leEmail string, ssl bool, userID int64) (int64, error) {
	bench, err := s.db.ReadQ.GetBench(ctx, benchID)
	if err != nil {
		return 0, fmt.Errorf("bench %d: %w", benchID, err)
	}
	// Refuse a domain that is already a site or route on this server.
	if err := s.ensureDomainFree(ctx, bench.ServerID, domain); err != nil {
		return 0, err
	}

	appsJSON, _ := json.Marshal(apps)
	site, err := s.db.WriteQ.CreateSite(ctx, store.CreateSiteParams{
		BenchID:   benchID,
		Domain:    domain,
		Status:    "creating",
		AppsJson:  string(appsJSON),
		SslEnabled: boolToInt(ssl),
	})
	if err != nil {
		return 0, err
	}

	// Store the per-site admin password and the bench db-root password.
	if err := s.putSecret(ctx, siteAdminSecretName(site.ID), adminPassword); err != nil {
		return 0, err
	}
	if err := s.putSecret(ctx, dbRootSecretName(benchID), dbRootPassword); err != nil {
		return 0, err
	}

	newSite := jobs.NewSiteParams{
		BenchPath:      bench.Path,
		Domain:         domain,
		Apps:           apps,
		AdminPassword:  jobs.SecretRef("secret:" + siteAdminSecretName(site.ID)),
		DBRootUser:     dbRootUser,
		DBRootPassword: jobs.SecretRef("secret:" + dbRootSecretName(benchID)),
		WithSSL:        ssl,
	}
	// new_site now folds nginx (setup + test + reload) in as trailing steps,
	// matching Frappe Press's single new_site job — so there is no separate
	// setup_nginx seed. The one job touches both the bench and the shared nginx
	// config, so it holds TWO locks: the bench lock (serializes bench work) and
	// the nginx lock (serializes against routes/cert issuance on the server).
	seeds := []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeNewSite, params: newSite,
			lockKey: benchLock(benchID), lockKey2: nginxLock(bench.ServerID)},
	}
	if ssl {
		seeds = append(seeds, jobSeed{
			serverID: bench.ServerID,
			typ:      jobs.TypeIssueCertificate,
			params: jobs.IssueCertificateParams{
				Domain: domain, Target: jobs.CertTargetSite, Email: leEmail,
				BenchPath: bench.Path, TestCert: s.cfg.Dev,
			},
			lockKey: nginxLock(bench.ServerID),
		})
	}
	depID, err := s.createDeploy(ctx, "site_create", "site", site.ID, "ui", "", userID, seeds)
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// DeleteSite marks the site as archived, enqueues a delete_site job, and
// removes the site row after the job succeeds (handled by finishDeploys).
func (s *Server) DeleteSite(ctx context.Context, siteID int64, dbRootUser, dbRootPassword string, userID int64) (int64, error) {
	site, err := s.db.ReadQ.GetSite(ctx, siteID)
	if err != nil {
		return 0, fmt.Errorf("site %d: %w", siteID, err)
	}
	bench, err := s.db.ReadQ.GetBench(ctx, site.BenchID)
	if err != nil {
		return 0, fmt.Errorf("bench %d: %w", site.BenchID, err)
	}
	secretName := dbRootSecretName(bench.ID)
	if err := s.putSecret(ctx, secretName, dbRootPassword); err != nil {
		return 0, fmt.Errorf("store secret: %w", err)
	}
	if err := s.db.WriteQ.ArchiveSite(ctx, siteID); err != nil {
		return 0, err
	}
	params := jobs.DeleteSiteParams{
		BenchPath:      bench.Path,
		Domain:         site.Domain,
		DBRootUser:     dbRootUser,
		DBRootPassword: jobs.SecretRef("secret:" + secretName),
	}
	depID, err := s.createDeploy(ctx, "site_delete", "site", siteID, "ui", "", userID, []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeDeleteSite, params: params, lockKey: benchLock(bench.ID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// BackupSite builds a one-job backup deploy.
func (s *Server) BackupSite(ctx context.Context, siteID int64, withFiles bool, userID int64) (int64, error) {
	site, err := s.db.ReadQ.GetSite(ctx, siteID)
	if err != nil {
		return 0, err
	}
	bench, err := s.db.ReadQ.GetBench(ctx, site.BenchID)
	if err != nil {
		return 0, err
	}
	params := jobs.BackupSiteParams{BenchPath: bench.Path, Site: site.Domain, WithFiles: withFiles}
	depID, err := s.createDeploy(ctx, "backup", "site", site.ID, "ui", "", userID, []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeBackupSite, params: params, lockKey: benchLock(bench.ID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// RollbackFrappeApp reverts an app on a bench to its last-recorded deploy point:
// checks out the previous commit and restores the pre-deploy database backups.
// Requires a deploy point recorded by a prior successful deploy.
func (s *Server) RollbackFrappeApp(ctx context.Context, benchID int64, appName string, userID int64) (int64, error) {
	bench, err := s.db.ReadQ.GetBench(ctx, benchID)
	if err != nil {
		return 0, fmt.Errorf("bench %d: %w", benchID, err)
	}
	dp, err := s.db.ReadQ.GetDeployPoint(ctx, store.GetDeployPointParams{
		BenchID: benchID, AppName: appName,
	})
	if err != nil {
		return 0, fmt.Errorf("no rollback point recorded for %s on this bench", appName)
	}
	var backups map[string]string
	_ = json.Unmarshal([]byte(dp.BackupsJson), &backups)

	// Branch comes from the app source (match by name).
	branch := "main"
	if src, err := s.db.ReadQ.GetAppSourceByName(ctx, appName); err == nil {
		branch = src.Branch
	}

	// Sites currently active on the bench with the app.
	sites, err := s.db.ReadQ.SitesForBench(ctx, benchID)
	if err != nil {
		return 0, err
	}
	var domains []string
	for _, st := range sites {
		if st.Status == "active" {
			domains = append(domains, st.Domain)
		}
	}
	if len(domains) == 0 {
		return 0, fmt.Errorf("no active sites on this bench to roll back")
	}

	params := jobs.RollbackFrappeAppParams{
		BenchPath: bench.Path,
		App:       appName,
		Branch:    branch,
		Commit:    dp.PrevCommit,
		Sites:     domains,
		Backups:   backups,
		CloneTok:  jobs.SecretRef("secret:" + cloneTokenSecretName),
	}
	depID, err := s.createDeploy(ctx, "frappe_rollback", "bench", benchID, "ui", dp.PrevCommit, userID, []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeRollbackFrappeApp, params: params, lockKey: benchLock(benchID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// benchAppGit returns the stored git state (branch, remote owner/repo) for an
// app on a bench, falling back to a linked app source when the agent could not
// read .git. Returns ("","") when nothing is known.
func (s *Server) benchAppGit(ctx context.Context, benchID int64, appName string) (branch, repo string) {
	apps, _ := s.db.ReadQ.ListFrappeAppsForBench(ctx, benchID)
	for _, a := range apps {
		if a.AppName != appName {
			continue
		}
		if a.GitBranch.Valid {
			branch = a.GitBranch.String
		}
		if a.GitRemote.Valid {
			repo = a.GitRemote.String
		}
	}
	if branch == "" || repo == "" {
		if src, err := s.db.ReadQ.GetAppSourceByName(ctx, appName); err == nil {
			if branch == "" {
				branch = src.Branch
			}
			if repo == "" {
				repo = src.Repo
			}
		}
	}
	return branch, repo
}

// UpdateFrappeApp updates an existing app on a bench to a target commit (or the
// latest on its remote branch when commit is empty), running the full
// deploy_frappe_app sequence (backup -> fetch -> checkout -> deps -> migrate ->
// build -> restart) across every active site on the bench. maintenance and
// skipPatches flow into the migrate behavior.
func (s *Server) UpdateFrappeApp(ctx context.Context, benchID int64, appName, commit string, hasPackage, maintenance, skipPatches bool, userID int64) (int64, error) {
	bench, err := s.db.ReadQ.GetBench(ctx, benchID)
	if err != nil {
		return 0, fmt.Errorf("bench %d: %w", benchID, err)
	}
	branch, repo := s.benchAppGit(ctx, benchID, appName)
	if branch == "" {
		return 0, fmt.Errorf("no branch known for %s on this bench; link an app source first", appName)
	}
	// Resolve "latest" when no explicit commit was given.
	if commit == "" {
		if repo == "" || !validRepo(repo) {
			return 0, fmt.Errorf("cannot resolve latest commit: no GitHub remote known for %s", appName)
		}
		commit = s.ghLatestCommit(ctx, repo, branch)
		if commit == "" {
			return 0, fmt.Errorf("could not fetch latest commit for %s@%s (check GitHub token access)", repo, branch)
		}
	}

	sites, err := s.db.ReadQ.SitesForBench(ctx, benchID)
	if err != nil {
		return 0, err
	}
	// Only migrate/build the active sites that actually have this app installed.
	// The code checkout is bench-wide, but migrate+build must touch just the
	// sites where the app exists — a site without the app has no schema to
	// migrate and would error.
	var domains []string
	for _, st := range sites {
		if st.Status != "active" {
			continue
		}
		var installed []string
		_ = json.Unmarshal([]byte(st.AppsJson), &installed)
		if slices.Contains(installed, appName) {
			domains = append(domains, st.Domain)
		}
	}
	if len(domains) == 0 {
		return 0, fmt.Errorf("no active site on this bench has %s installed", appName)
	}

	params := jobs.DeployFrappeAppParams{
		BenchPath:   bench.Path,
		App:         appName,
		Branch:      branch,
		Commit:      commit,
		Sites:       domains,
		HasPackage:  hasPackage,
		Maintenance: maintenance,
		SkipPatches: skipPatches,
		CloneTok:    jobs.SecretRef("secret:" + cloneTokenSecretName),
	}
	depID, err := s.createDeploy(ctx, "frappe_update", "bench", benchID, "ui", commit, userID, []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeDeployFrappeApp, params: params, lockKey: benchLock(benchID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// InstallAppSpec is one app to install. Repo (owner/name) or URL (full clone
// URL) are only needed when the app's code is not already on the bench; Branch
// is required in that case too.
type InstallAppSpec struct {
	App    string
	Repo   string
	URL    string
	Branch string
}

// InstallFrappeApp installs one or more apps onto one or more sites on a bench
// in a single deploy. For each requested app whose code is not yet on the bench,
// a get_frappe_app job runs once (fetch via URL or owner/name); then an
// install_app job runs for every (app, site) pair. All jobs share the bench
// lock so they run in order.
func (s *Server) InstallFrappeApp(ctx context.Context, benchID int64, siteIDs []int64, apps []InstallAppSpec, userID int64) (depID int64, skipped []string, err error) {
	bench, err := s.db.ReadQ.GetBench(ctx, benchID)
	if err != nil {
		return 0, nil, fmt.Errorf("bench %d: %w", benchID, err)
	}
	if len(apps) == 0 {
		return 0, nil, fmt.Errorf("no apps selected")
	}
	if len(siteIDs) == 0 {
		return 0, nil, fmt.Errorf("no sites selected")
	}

	// Resolve + validate every target site belongs to this bench.
	var siteDomains []string
	for _, id := range siteIDs {
		site, err := s.db.ReadQ.GetSite(ctx, id)
		if err != nil {
			return 0, nil, fmt.Errorf("site %d: %w", id, err)
		}
		if site.BenchID != benchID {
			return 0, nil, fmt.Errorf("site %s does not belong to this bench", site.Domain)
		}
		siteDomains = append(siteDomains, site.Domain)
	}

	// Which apps (and which installed-on sites) already exist on the bench?
	onBench := map[string]bool{}
	existing, _ := s.db.ReadQ.ListFrappeAppsForBench(ctx, benchID)
	for _, a := range existing {
		onBench[a.AppName] = true
	}

	var seeds []jobSeed
	// 1. Fetch each app that is not yet on the bench (once per app). Apps whose
	//    code already exists on the bench are NOT re-fetched — a second
	//    `bench get-app` of an existing app conflicts/fails, so we install from
	//    the checkout already present and report them as skipped-fetch.
	for _, a := range apps {
		if a.App == "" {
			return 0, nil, fmt.Errorf("app name is required")
		}
		if onBench[a.App] {
			if a.URL != "" || a.Repo != "" {
				// The operator supplied a source for an app already on the bench.
				// We keep the existing checkout rather than clobbering it.
				skipped = append(skipped, a.App)
			}
			continue
		}
		if a.Branch == "" || (a.Repo == "" && a.URL == "") {
			return 0, nil, fmt.Errorf("app %s is not on the bench; a repo or URL and a branch are required to fetch it", a.App)
		}
		// Validate the source up front (app name, branch, and repo-or-URL) so a
		// bad/unsafe value is rejected before any deploy row is written.
		if err := jobs.ValidateFetchSource(a.App, a.Repo, a.URL, a.Branch); err != nil {
			return 0, nil, err
		}
		seeds = append(seeds, jobSeed{
			serverID: bench.ServerID, typ: jobs.TypeGetFrappeApp,
			params: jobs.GetFrappeAppParams{
				BenchPath: bench.Path, Repo: a.Repo, URL: a.URL, Branch: a.Branch,
				CloneTok: jobs.SecretRef("secret:" + cloneTokenSecretName),
			},
			lockKey: benchLock(benchID),
		})
	}
	// 2. Install every app on every selected site.
	for _, a := range apps {
		for _, domain := range siteDomains {
			seeds = append(seeds, jobSeed{
				serverID: bench.ServerID, typ: jobs.TypeInstallApp,
				params:  jobs.InstallAppParams{BenchPath: bench.Path, Site: domain, App: a.App},
				lockKey: benchLock(benchID),
			})
		}
	}

	// Target the deploy at the first site (deploys reference a single target).
	depID, err = s.createDeploy(ctx, "app_install", "site", siteIDs[0], "ui", "", userID, seeds)
	if err != nil {
		return 0, nil, err
	}
	s.nudge()
	return depID, skipped, nil
}

// UninstallFrappeApp removes an app from one site's database (uninstall_app). It
// leaves the app code on the bench since other sites may use it.
func (s *Server) UninstallFrappeApp(ctx context.Context, siteID int64, appName string, force bool, userID int64) (int64, error) {
	site, err := s.db.ReadQ.GetSite(ctx, siteID)
	if err != nil {
		return 0, fmt.Errorf("site %d: %w", siteID, err)
	}
	bench, err := s.db.ReadQ.GetBench(ctx, site.BenchID)
	if err != nil {
		return 0, fmt.Errorf("bench %d: %w", site.BenchID, err)
	}
	if appName == "frappe" {
		return 0, fmt.Errorf("the frappe app cannot be uninstalled")
	}
	params := jobs.UninstallAppParams{
		BenchPath: bench.Path, Site: site.Domain, App: appName, Force: force,
	}
	depID, err := s.createDeploy(ctx, "app_uninstall", "site", siteID, "ui", "", userID, []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeUninstallApp, params: params, lockKey: benchLock(bench.ID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// ApplyRoute builds a one-job write_proxy_route deploy, and when ssl is set and
// a cert is needed, an issue_certificate + re-apply follow-up.
func (s *Server) ApplyRoute(ctx context.Context, routeID int64, leEmail string, userID int64) (int64, error) {
	route, err := s.db.ReadQ.GetProxyRoute(ctx, routeID)
	if err != nil {
		return 0, err
	}
	if route.Managed == 0 {
		// Unmanaged routes are discovered from existing nginx vhosts and shown
		// read-only; RedCi must never re-render and overwrite a hand-tuned config.
		return 0, fmt.Errorf("route %q is unmanaged (discovered from an existing nginx config); RedCi will not overwrite it", route.Domain)
	}
	ssl := route.SslEnabled != 0
	snippet := ""
	if route.ExtraSnippet.Valid {
		snippet = route.ExtraSnippet.String
	}

	var seeds []jobSeed
	if ssl {
		// Write HTTP-only first, issue the cert, then re-apply with SSL.
		seeds = append(seeds,
			jobSeed{serverID: route.ServerID, typ: jobs.TypeWriteProxyRoute,
				params: writeRouteParams(route, snippet, false), lockKey: nginxLock(route.ServerID)},
			jobSeed{serverID: route.ServerID, typ: jobs.TypeIssueCertificate,
				params: jobs.IssueCertificateParams{Domain: route.Domain, Target: jobs.CertTargetRoute, Email: leEmail, TestCert: s.cfg.Dev},
				lockKey: nginxLock(route.ServerID)},
			jobSeed{serverID: route.ServerID, typ: jobs.TypeWriteProxyRoute,
				params: writeRouteParams(route, snippet, true), lockKey: nginxLock(route.ServerID)},
		)
	} else {
		seeds = append(seeds, jobSeed{serverID: route.ServerID, typ: jobs.TypeWriteProxyRoute,
			params: writeRouteParams(route, snippet, false), lockKey: nginxLock(route.ServerID)})
	}
	depID, err := s.createDeploy(ctx, "proxy_route", "route", route.ID, "ui", "", userID, seeds)
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// CreateRoute inserts a proxy route (simple or Model B path-split) and returns
// its id. It does not apply it — call ApplyRoute next. Locations empty = simple
// single-upstream proxy; non-empty = a full path-split vhost.
func (s *Server) CreateRoute(ctx context.Context, serverID int64, p jobs.WriteProxyRouteParams) (int64, error) {
	return s.createRouteLinked(ctx, serverID, p, 0)
}

// CreateRouteForWebApp links a route to a registered web app: the upstream is
// derived from the app's internal_port (127.0.0.1:<port>) so the panel is the
// single source of truth, and web_app_id is persisted so the app and its nginx
// config are connected both ways. The app must live on the same server.
func (s *Server) CreateRouteForWebApp(ctx context.Context, webAppID int64, p jobs.WriteProxyRouteParams) (int64, error) {
	app, err := s.db.ReadQ.GetWebApp(ctx, webAppID)
	if err != nil {
		return 0, fmt.Errorf("web app %d not found", webAppID)
	}
	// Auto-derive the upstream from the app's internal port (caller may leave it
	// blank; an explicit upstream is honoured so the form can prefill+edit it).
	if p.Upstream == "" {
		p.Upstream = fmt.Sprintf("http://127.0.0.1:%d", app.InternalPort)
	}
	return s.createRouteLinked(ctx, app.ServerID, p, webAppID)
}

// createRouteLinked is the shared insert for CreateRoute / CreateRouteForWebApp.
// webAppID == 0 means "no link" (stored as NULL).
func (s *Server) createRouteLinked(ctx context.Context, serverID int64, p jobs.WriteProxyRouteParams, webAppID int64) (int64, error) {
	if err := p.Validate(nil); err != nil {
		return 0, err
	}
	if err := s.ensureDomainFree(ctx, serverID, p.Domain); err != nil {
		return 0, err
	}
	link := sql.NullInt64{}
	if webAppID > 0 {
		link = sql.NullInt64{Int64: webAppID, Valid: true}
	}
	// Raw mode: store the verbatim vhost; the agent writes it as-is. managed=1
	// (an authored raw route, unlike a discovered/unmanaged one).
	if p.RawConfig != "" {
		rt, err := s.db.WriteQ.CreateProxyRouteRaw(ctx, store.CreateProxyRouteRawParams{
			ServerID: serverID, Domain: p.Domain, WebAppID: link,
			SslEnabled: boolToInt(p.SSL), RawConfig: nullStr(p.RawConfig),
		})
		if err != nil {
			return 0, err
		}
		return rt.ID, nil
	}
	// The simple-mode upstream column still stores p.Upstream (may be empty in
	// Model B); the catch-all in Model B lives in Locations instead.
	if len(p.Locations) == 0 {
		rt, err := s.db.WriteQ.CreateProxyRoute(ctx, store.CreateProxyRouteParams{
			ServerID: serverID, Domain: p.Domain, Upstream: p.Upstream,
			WebAppID:   link,
			SslEnabled: boolToInt(p.SSL), ExtraSnippet: nullStr(p.Snippet),
		})
		if err != nil {
			return 0, err
		}
		return rt.ID, nil
	}
	locJSON, _ := json.Marshal(p.Locations)
	aliasesJSON, _ := json.Marshal(p.Aliases)
	rt, err := s.db.WriteQ.CreateProxyRouteModelB(ctx, store.CreateProxyRouteModelBParams{
		ServerID:      serverID,
		Domain:        p.Domain,
		Upstream:      p.Upstream,
		WebAppID:      link,
		SslEnabled:    boolToInt(p.SSL),
		ExtraSnippet:  nullStr(p.Snippet),
		LocationsJson: nullStr(string(locJSON)),
		FrappeSite:    nullStr(p.FrappeSite),
		StaticRoot:    nullStr(p.StaticRoot),
		AliasesJson:   nullStr(string(aliasesJSON)),
	})
	if err != nil {
		return 0, err
	}
	return rt.ID, nil
}

// DeleteRoute builds a one-job delete_proxy_route deploy.
func (s *Server) DeleteRoute(ctx context.Context, routeID int64, userID int64) (int64, error) {
	route, err := s.db.ReadQ.GetProxyRoute(ctx, routeID)
	if err != nil {
		return 0, err
	}
	if route.Managed == 0 {
		// Never run a delete_proxy_route job for a discovered vhost: RedCi did not
		// write that file and must not remove it. To stop tracking an unmanaged
		// route, delete only its panel record (see ForgetRoute), which leaves the
		// live nginx config on the server untouched.
		return 0, fmt.Errorf("route %q is unmanaged (discovered); use Forget to remove it from the panel without touching nginx", route.Domain)
	}
	depID, err := s.createDeploy(ctx, "proxy_route", "route", route.ID, "ui", "", userID, []jobSeed{
		{serverID: route.ServerID, typ: jobs.TypeDeleteProxyRoute,
			params: jobs.DeleteProxyRouteParams{Domain: route.Domain}, lockKey: nginxLock(route.ServerID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

func writeRouteParams(route store.ProxyRoute, snippet string, ssl bool) jobs.WriteProxyRouteParams {
	p := jobs.WriteProxyRouteParams{
		RouteID:  route.ID,
		Domain:   route.Domain,
		Upstream: route.Upstream,
		SSL:      ssl,
		Snippet:  snippet,
	}
	// Model B fields (populated only for path-split routes).
	if route.FrappeSite.Valid {
		p.FrappeSite = route.FrappeSite.String
	}
	if route.StaticRoot.Valid {
		p.StaticRoot = route.StaticRoot.String
	}
	if route.AliasesJson.Valid && route.AliasesJson.String != "" {
		_ = json.Unmarshal([]byte(route.AliasesJson.String), &p.Aliases)
	}
	if route.LocationsJson.Valid && route.LocationsJson.String != "" {
		_ = json.Unmarshal([]byte(route.LocationsJson.String), &p.Locations)
	}
	// Raw mode: carry the verbatim vhost so the agent writes it as-is.
	if route.RawConfig.Valid && route.RawConfig.String != "" {
		p.RawConfig = route.RawConfig.String
	}
	return p
}

// ensureDomainFree refuses a domain already used by a site or route.
func (s *Server) ensureDomainFree(ctx context.Context, serverID int64, domain string) error {
	n, err := s.db.ReadQ.RouteOrSiteExistsForDomain(ctx, store.RouteOrSiteExistsForDomainParams{
		ServerID: serverID, Domain: domain, Domain_2: domain,
	})
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("domain %q is already a site or route", domain)
	}
	return nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
