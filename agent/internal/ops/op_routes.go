package ops

import (
	"context"
	"encoding/json"

	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// runWriteRoute renders and atomically applies a proxy route, returning the
// new config hash for drift detection.
func runWriteRoute(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.WriteProxyRouteParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	content, err := jobs.RenderRoute(jobs.RouteData{
		ID:       p.RouteID,
		Domain:   p.Domain,
		Upstream: p.Upstream,
		SSL:      p.SSL,
		Snippet:  p.Snippet,
		HTTP2On:  nginxHasHTTP2On(env.NginxVer),
	})
	if err != nil {
		return fail(err)
	}
	hash, err := env.Nginx.Apply(ctx, content, p.Domain, log)
	if err != nil {
		return fail(err)
	}
	return ok(map[string]string{"config_hash": hash})
}

// runDeleteRoute removes a proxy route and reloads nginx.
func runDeleteRoute(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.DeleteProxyRouteParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	if err := env.Nginx.Delete(ctx, p.Domain, log); err != nil {
		return fail(err)
	}
	return ok(map[string]string{"domain": p.Domain})
}

// runListNginx returns every ops.d config file with its sha256.
func runListNginx(env Env, log Log) Result {
	files, err := env.Nginx.ListOpsConfigs()
	if err != nil {
		return fail(err)
	}
	log("system", "listed nginx configs")
	return ok(map[string]any{"configs": files})
}

// nginxHasHTTP2On reports whether the nginx version uses the `http2 on;`
// directive (1.25.1+) rather than the legacy `listen ... http2`.
func nginxHasHTTP2On(ver string) bool {
	maj, min, patch := parseNginxVersion(ver)
	if maj != 1 {
		return maj > 1
	}
	if min != 25 {
		return min > 25
	}
	return patch >= 1
}
