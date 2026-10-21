package server

import (
	"context"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
)

// effectiveSiteRole returns a user's effective role on a specific site. It is
// the HIGHEST of: the user's global role, any grant on that exact site, and any
// grant on the site's bench (a bench grant covers all its sites). Grants only
// ever raise access — never lower it — so a global admin stays admin everywhere.
func (s *Server) effectiveSiteRole(ctx context.Context, u storeUser, siteID int64) auth.Role {
	eff := auth.Role(u.Role)
	if eff == auth.RoleAdmin {
		return eff // global admin already maxed; skip the lookups
	}
	site, err := s.db.ReadQ.GetSite(ctx, siteID)
	if err != nil {
		return eff // unknown site: fall back to global role (handler will 404)
	}
	grants, err := s.db.ReadQ.GrantsForUser(ctx, u.ID)
	if err != nil {
		return eff
	}
	for _, g := range grants {
		match := (g.ResourceType == "site" && g.ResourceID == site.ID) ||
			(g.ResourceType == "bench" && g.ResourceID == site.BenchID)
		if match {
			eff = eff.Max(auth.Role(g.Role))
		}
	}
	return eff
}

// effectiveBenchRole returns a user's effective role on a bench: the highest of
// the global role and any grant on that bench. (Site grants do not bubble up to
// the bench — only down to sites.)
func (s *Server) effectiveBenchRole(ctx context.Context, u storeUser, benchID int64) auth.Role {
	eff := auth.Role(u.Role)
	if eff == auth.RoleAdmin {
		return eff
	}
	grants, err := s.db.ReadQ.GrantsForUser(ctx, u.ID)
	if err != nil {
		return eff
	}
	for _, g := range grants {
		if g.ResourceType == "bench" && g.ResourceID == benchID {
			eff = eff.Max(auth.Role(g.Role))
		}
	}
	return eff
}
