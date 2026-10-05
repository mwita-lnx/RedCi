package ops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// DiscoveredBench is a bench found under a bench root, reported from files
// alone so the panel can offer it for import. Nothing on the server changes.
type DiscoveredBench struct {
	Path  string             `json:"path"`
	Apps  []string           `json:"apps"`
	Sites []DiscoveredSite   `json:"sites"`
}

// DiscoveredSite is a site directory with a site_config.json.
type DiscoveredSite struct {
	Domain  string `json:"domain"`
	HasSSL  bool   `json:"has_ssl"`
}

// StatusResult is the result_json of a server_status job.
type StatusResult struct {
	NginxVersion  string            `json:"nginx_version"`
	BenchVersion  string            `json:"bench_version"`
	DockerVersion string            `json:"docker_version"`
	DiskUsedPct   int               `json:"disk_used_pct"`
	Benches       []DiscoveredBench `json:"benches"`
}

// runServerStatus collects versions and discovers benches/sites from files.
func runServerStatus(ctx context.Context, env Env, log Log) Result {
	res := StatusResult{}
	res.NginxVersion = env.NginxVer
	if res.NginxVersion == "" {
		res.NginxVersion = detectNginxVersion(ctx, env)
	}
	res.BenchVersion = firstWord(execOut(ctx, env, "bench", "version"))
	res.DockerVersion = firstWord(execOut(ctx, env, "docker", "--version"))

	for _, root := range env.BenchRoots {
		b, ok := discoverBench(root)
		if ok {
			res.Benches = append(res.Benches, b)
		}
	}
	log("system", "collected server status and discovered "+strconv.Itoa(len(res.Benches))+" bench(es)")
	return ok(res)
}

// discoverBench inspects a bench directory using only file reads.
func discoverBench(root string) (DiscoveredBench, bool) {
	appsTxt := filepath.Join(root, "sites", "apps.txt")
	data, err := os.ReadFile(appsTxt)
	if err != nil {
		return DiscoveredBench{}, false
	}
	b := DiscoveredBench{Path: root}
	for _, line := range strings.Split(string(data), "\n") {
		if a := strings.TrimSpace(line); a != "" {
			b.Apps = append(b.Apps, a)
		}
	}
	// Each directory under sites/ with a site_config.json is a site.
	sitesDir := filepath.Join(root, "sites")
	entries, _ := os.ReadDir(sitesDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cfgPath := filepath.Join(sitesDir, e.Name(), "site_config.json")
		cfg, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		b.Sites = append(b.Sites, DiscoveredSite{
			Domain: e.Name(),
			HasSSL: strings.Contains(string(cfg), "ssl_certificate"),
		})
	}
	return b, true
}

func detectNginxVersion(ctx context.Context, env Env) string {
	return DetectNginxVersion(ctx, env.Runner)
}

// DetectNginxVersion runs `nginx -v` (output like "nginx version: nginx/1.24.0")
// and returns the version string, or "" if it cannot be determined.
func DetectNginxVersion(ctx context.Context, r *runner.Runner) string {
	out, _, _ := r.Exec(ctx, jobs.Step{Command: "nginx", Args: []string{"-v"}}, 30*time.Second)
	if i := strings.Index(out, "nginx/"); i >= 0 {
		return strings.TrimSpace(out[i+len("nginx/"):])
	}
	return ""
}

func execOut(ctx context.Context, env Env, cmd string, args ...string) string {
	out, _, _ := env.Runner.Exec(ctx, jobs.Step{Command: cmd, Args: args}, 30*time.Second)
	return out
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	for _, w := range strings.Fields(s) {
		return w
	}
	return ""
}

// parseNginxVersion extracts major.minor.patch from a version string.
func parseNginxVersion(v string) (maj, min, patch int) {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	get := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return get(0), get(1), get(2)
}
