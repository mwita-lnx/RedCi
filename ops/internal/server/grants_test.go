package server_test

import (
	"context"
	"testing"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/paneltest"
)

// TestEffectiveSiteRole verifies per-resource RBAC: a global viewer is raised to
// deployer on a site they're granted, a bench grant covers the bench's sites,
// unrelated sites keep the global role, and a global admin stays admin.
func TestEffectiveSiteRole(t *testing.T) {
	ctx := context.Background()
	p, err := paneltest.Start(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// A global viewer.
	u, err := p.DB.WriteQ.CreateUser(ctx, store.CreateUserParams{
		Email: "v@example.com", Name: "V", PasswordHash: "x", Role: "viewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A server + bench + two sites on it.
	srv, _, err := p.CreateEnrollment(ctx, "s1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	bench, err := p.DB.WriteQ.CreateBench(ctx, store.CreateBenchParams{ServerID: srv, Name: "b", Path: "/b"})
	if err != nil {
		t.Fatal(err)
	}
	siteA, err := p.DB.WriteQ.CreateSite(ctx, store.CreateSiteParams{BenchID: bench.ID, Domain: "a.test", Status: "active", AppsJson: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	siteB, err := p.DB.WriteQ.CreateSite(ctx, store.CreateSiteParams{BenchID: bench.ID, Domain: "b.test", Status: "active", AppsJson: "[]"})
	if err != nil {
		t.Fatal(err)
	}

	roleOn := func(siteID int64) string {
		r, err := p.Server.EffectiveSiteRoleForTest(ctx, u.ID, siteID)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return r
	}

	// Baseline: global viewer everywhere.
	if got := roleOn(siteA.ID); got != "viewer" {
		t.Errorf("baseline siteA = %q, want viewer", got)
	}

	// Site grant raises only that site.
	if err := p.Server.GrantResource(ctx, u.ID, "site", siteA.ID, "deployer"); err != nil {
		t.Fatal(err)
	}
	if got := roleOn(siteA.ID); got != "deployer" {
		t.Errorf("after site grant, siteA = %q, want deployer", got)
	}
	if got := roleOn(siteB.ID); got != "viewer" {
		t.Errorf("siteB unaffected = %q, want viewer", got)
	}

	// Bench grant covers all its sites (and never lowers: siteA stays deployer).
	if err := p.Server.GrantResource(ctx, u.ID, "bench", bench.ID, "deployer"); err != nil {
		t.Fatal(err)
	}
	if got := roleOn(siteB.ID); got != "deployer" {
		t.Errorf("after bench grant, siteB = %q, want deployer", got)
	}

	// A global admin is admin everywhere regardless of grants.
	admin, err := p.DB.WriteQ.CreateUser(ctx, store.CreateUserParams{
		Email: "admin@example.com", Name: "A", PasswordHash: "x", Role: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Server.EffectiveSiteRoleForTest(ctx, admin.ID, siteA.ID); got != "admin" {
		t.Errorf("admin on siteA = %q, want admin", got)
	}
}
