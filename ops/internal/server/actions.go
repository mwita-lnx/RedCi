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
func (s *Server) CreateSiteDeploy(ctx context.Context, benchID int64, domain string, apps []string, adminPassword, dbRootPassword, leEmail string, ssl bool, userID int64) (int64, error) {
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
		DBRootPassword: jobs.SecretRef("secret:" + dbRootSecretName(benchID)),
		WithSSL:        ssl,
	}
	seeds := []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeNewSite, params: newSite, lockKey: benchLock(benchID)},
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
