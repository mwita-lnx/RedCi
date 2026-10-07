package server

import (
	"context"
	"encoding/json"
	"fmt"

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
	seeds := []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeNewSite, params: newSite, lockKey: benchLock(benchID)},
		{serverID: bench.ServerID, typ: jobs.TypeSetupNginx, params: jobs.SetupNginxParams{
			BenchPath: bench.Path, Domain: domain,
		}, lockKey: nginxLock(bench.ServerID)},
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

// ApplyRoute builds a one-job write_proxy_route deploy, and when ssl is set and
// a cert is needed, an issue_certificate + re-apply follow-up.
func (s *Server) ApplyRoute(ctx context.Context, routeID int64, leEmail string, userID int64) (int64, error) {
	route, err := s.db.ReadQ.GetProxyRoute(ctx, routeID)
	if err != nil {
		return 0, err
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

// DeleteRoute builds a one-job delete_proxy_route deploy.
func (s *Server) DeleteRoute(ctx context.Context, routeID int64, userID int64) (int64, error) {
	route, err := s.db.ReadQ.GetProxyRoute(ctx, routeID)
	if err != nil {
		return 0, err
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
	return jobs.WriteProxyRouteParams{
		RouteID:  route.ID,
		Domain:   route.Domain,
		Upstream: route.Upstream,
		SSL:      ssl,
		Snippet:  snippet,
	}
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
