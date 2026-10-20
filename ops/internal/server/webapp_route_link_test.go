package server_test

import (
	"context"
	"testing"

	"github.com/mwita-lnx/RedCi/ops/internal/server"
	"github.com/mwita-lnx/RedCi/ops/paneltest"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// TestCreateRouteForWebApp verifies that linking a route to a registered web app
// derives the upstream from the app's internal_port and persists web_app_id, so
// the app and its nginx config are connected both ways.
func TestCreateRouteForWebApp(t *testing.T) {
	ctx := context.Background()
	p, err := paneltest.Start(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	serverID, _, err := p.CreateEnrollment(ctx, "s1", "s1")
	if err != nil {
		t.Fatal(err)
	}

	appID, err := p.Server.CreateWebApp(ctx, server.WebAppSpec{
		ServerID: serverID, Name: "shop", Runtime: "docker",
		Repo: "acme/shop", Branch: "main", InternalPort: 3500, HealthPath: "/",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Leave upstream blank → must be auto-derived from the app's internal_port.
	routeID, err := p.Server.CreateRouteForWebApp(ctx, appID, jobs.WriteProxyRouteParams{
		Domain: "shop.example.com", SSL: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	rt, err := p.DB.ReadQ.GetProxyRoute(ctx, routeID)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Upstream != "http://127.0.0.1:3500" {
		t.Errorf("upstream = %q, want http://127.0.0.1:3500 (derived from internal_port)", rt.Upstream)
	}
	if !rt.WebAppID.Valid || rt.WebAppID.Int64 != appID {
		t.Errorf("web_app_id = %v, want %d", rt.WebAppID, appID)
	}
	if rt.ServerID != serverID {
		t.Errorf("server_id = %d, want %d (from the app)", rt.ServerID, serverID)
	}
	if rt.Managed == 0 {
		t.Error("a web-app route must be managed (managed=1)")
	}

	// Reverse link: the app should report this route.
	routes, err := p.DB.ReadQ.RoutesForWebApp(ctx, rt.WebAppID)
	if err != nil || len(routes) != 1 || routes[0].ID != routeID {
		t.Errorf("RoutesForWebApp = %v (err %v), want the one route %d", routes, err, routeID)
	}
}
