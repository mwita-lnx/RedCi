package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// DiscoveredBench is a bench found under a bench root, reported from files
// alone so the panel can offer it for import. Nothing on the server changes.
type DiscoveredBench struct {
	Path        string            `json:"path"`
	Apps        []string          `json:"apps"`
	AppVersions map[string]string `json:"app_versions,omitempty"`
	Sites       []DiscoveredSite  `json:"sites"`
	Facts       BenchFacts        `json:"facts"`
}

// BenchFacts are runtime details read from a bench's config/Procfile — the
// data behind the "Benches & sites" cards. All best-effort from files.
type BenchFacts struct {
	PythonVersion string          `json:"python_version,omitempty"`
	NodeVersion   string          `json:"node_version,omitempty"`
	DBType        string          `json:"db_type,omitempty"`   // mariadb | postgres
	DBHost        string          `json:"db_host,omitempty"`
	WebWorkers    int             `json:"web_workers"`
	RQWorkers     int             `json:"rq_workers"`
	SchedulerOn   bool            `json:"scheduler_on"`
	Queues        map[string]int  `json:"queues,omitempty"`    // queue -> pending (best-effort)
}

// DiscoveredSite is a site directory with a site_config.json.
type DiscoveredSite struct {
	Domain         string   `json:"domain"`
	HasSSL         bool     `json:"has_ssl"`
	InstalledApps  []string `json:"installed_apps"`
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
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		var cfg struct {
			InstalledApps []string `json:"installed_apps"`
			SSLCert       string   `json:"ssl_certificate"`
		}
		_ = json.Unmarshal(raw, &cfg)
		b.Sites = append(b.Sites, DiscoveredSite{
			Domain:        e.Name(),
			HasSSL:        cfg.SSLCert != "",
			InstalledApps: cfg.InstalledApps,
		})
	}
	b.AppVersions = readBenchAppVersions(root, b.Apps)
	b.Facts = readBenchFacts(root)
	return b, true
}

// readBenchFacts gathers runtime details for the bench from files only:
// Procfile (worker/scheduler topology), common_site_config.json (DB), and the
// bundled Python/Node version markers.
func readBenchFacts(root string) BenchFacts {
	f := BenchFacts{Queues: map[string]int{}}

	// Procfile: count web workers (gunicorn -w N or "web:" lines) and RQ workers.
	if proc, err := os.ReadFile(filepath.Join(root, "Procfile")); err == nil {
		text := string(proc)
		for _, line := range strings.Split(text, "\n") {
			l := strings.TrimSpace(line)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			lower := strings.ToLower(l)
			if strings.HasPrefix(lower, "web:") {
				// gunicorn ... -w <N>
				if m := regexp.MustCompile(`-w\s+(\d+)`).FindStringSubmatch(l); m != nil {
					f.WebWorkers, _ = strconv.Atoi(m[1])
				} else {
					f.WebWorkers = 1
				}
			}
			if strings.Contains(lower, "worker") && (strings.Contains(lower, "rq") || strings.Contains(lower, "bench worker")) {
				f.RQWorkers++
			}
			if strings.HasPrefix(lower, "schedule:") || strings.Contains(lower, "schedule") {
				f.SchedulerOn = true
			}
		}
	}

	// common_site_config.json: DB type/host + queue hints.
	if raw, err := os.ReadFile(filepath.Join(root, "sites", "common_site_config.json")); err == nil {
		var cfg struct {
			DBHost     string `json:"db_host"`
			DBType     string `json:"db_type"`
			RedisQueue string `json:"redis_queue"`
			Scheduler  int    `json:"pause_scheduler"`
		}
		_ = json.Unmarshal(raw, &cfg)
		f.DBHost = cfg.DBHost
		f.DBType = cfg.DBType
		if f.DBType == "" {
			f.DBType = "mariadb"
		}
		if cfg.Scheduler == 0 && f.SchedulerOn {
			f.SchedulerOn = true
		}
	}

	// Python version from the bundled virtualenv.
	if out, err := os.ReadFile(filepath.Join(root, "env", "pyvenv.cfg")); err == nil {
		if m := regexp.MustCompile(`version\s*=\s*([\d.]+)`).FindStringSubmatch(string(out)); m != nil {
			f.PythonVersion = m[1]
		}
	}
	// Node version from a bundled .nvmrc or node_version marker (best-effort).
	for _, p := range []string{".nvmrc", "node_version"} {
		if out, err := os.ReadFile(filepath.Join(root, p)); err == nil {
			f.NodeVersion = strings.TrimSpace(strings.TrimPrefix(string(out), "v"))
			break
		}
	}
	return f
}

var versionRe = regexp.MustCompile(`__version__\s*=\s*["']([^"']+)["']`)

func readBenchAppVersions(benchPath string, apps []string) map[string]string {
	versions := make(map[string]string, len(apps))
	for _, app := range apps {
		path := filepath.Join(benchPath, "apps", app, app, "__init__.py")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if m := versionRe.FindSubmatch(data); len(m) == 2 {
			versions[app] = string(m[1])
		}
	}
	return versions
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
