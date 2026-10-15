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
	AppGit      map[string]AppGit `json:"app_git,omitempty"` // per-app git state, read from .git files
	Sites       []DiscoveredSite  `json:"sites"`
	Facts       BenchFacts        `json:"facts"`
}

// AppGit is the on-disk git state of one app under apps/<name>, read purely from
// the .git directory (no git subprocess): the checked-out branch, the HEAD
// commit SHA, and the branch's remote tracking URL (normalized to owner/repo).
type AppGit struct {
	Branch    string `json:"branch,omitempty"`
	Commit    string `json:"commit,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"` // normalized owner/repo when GitHub
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

// DiscoverBenches does the cheap, file-only half of server_status: it walks the
// bench roots and reads benches/sites from disk, with NO version subprocesses
// (bench/docker/nginx). The agent's periodic discovery push uses this so it can
// run every minute without risk of hanging on `bench version` when the bench is
// busy. The result has the same shape as a server_status job, with version
// fields left empty (the panel preserves any previously-probed versions).
func DiscoverBenches(roots []string) StatusResult {
	res := StatusResult{}
	for _, root := range roots {
		if b, ok := discoverBench(root); ok {
			res.Benches = append(res.Benches, b)
		}
	}
	return res
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
	b.AppGit = readBenchAppGit(root, b.Apps)
	b.Facts = readBenchFacts(root)
	return b, true
}

// readBenchAppGit reads each app's git state from apps/<app>/.git using only
// file reads (no git subprocess, so it is safe inside the periodic push):
//   - .git/HEAD              -> "ref: refs/heads/<branch>" (branch name)
//   - .git/refs/heads/<br>   -> the commit SHA (falls back to packed-refs)
//   - .git/config            -> the branch's remote URL, normalized to owner/repo
func readBenchAppGit(benchPath string, apps []string) map[string]AppGit {
	out := make(map[string]AppGit, len(apps))
	for _, app := range apps {
		gitDir := filepath.Join(benchPath, "apps", app, ".git")
		if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
			continue
		}
		var g AppGit

		// HEAD → branch name (and commit if detached).
		head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
		if err != nil {
			continue
		}
		headStr := strings.TrimSpace(string(head))
		if ref := strings.TrimPrefix(headStr, "ref: "); ref != headStr {
			g.Branch = strings.TrimPrefix(ref, "refs/heads/")
			g.Commit = readGitRef(gitDir, ref)
		} else {
			// Detached HEAD: the file holds the SHA directly.
			g.Commit = headStr
		}

		// Remote URL for the branch's tracking remote (default "origin").
		g.RemoteURL = readGitRemote(gitDir, g.Branch)

		if g.Branch != "" || g.Commit != "" || g.RemoteURL != "" {
			out[app] = g
		}
	}
	return out
}

// readGitRef resolves a ref (e.g. refs/heads/version-16) to a commit SHA,
// checking the loose ref file first, then packed-refs.
func readGitRef(gitDir, ref string) string {
	if data, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
		return strings.TrimSpace(string(data))
	}
	if data, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			if fields := strings.Fields(line); len(fields) == 2 && fields[1] == ref {
				return fields[0]
			}
		}
	}
	return ""
}

// readGitRemote reads .git/config and returns the URL of the remote that the
// given branch tracks (or "origin"), normalized to owner/repo for GitHub URLs.
func readGitRemote(gitDir, branch string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return ""
	}
	// Minimal INI parse: collect [remote "X"].url and [branch "Y"].remote.
	remoteURLs := map[string]string{}
	branchRemote := map[string]string{}
	var section, subsection string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inner := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if i := strings.Index(inner, "\""); i >= 0 {
				section = strings.TrimSpace(inner[:i])
				subsection = strings.Trim(inner[i:], "\"")
			} else {
				section, subsection = strings.TrimSpace(inner), ""
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch section {
		case "remote":
			if k == "url" {
				remoteURLs[subsection] = v
			}
		case "branch":
			if k == "remote" {
				branchRemote[subsection] = v
			}
		}
	}
	// Prefer the branch's tracking remote, then "origin". If neither resolves
	// (e.g. the repo only defines an "upstream" remote and the branch has no
	// tracking config), fall back to the sole remote when there is exactly one.
	remote := branchRemote[branch]
	if remote == "" {
		remote = "origin"
	}
	if url := remoteURLs[remote]; url != "" {
		return normalizeRepo(url)
	}
	if len(remoteURLs) == 1 {
		for _, url := range remoteURLs {
			return normalizeRepo(url)
		}
	}
	return ""
}

// normalizeRepo turns a git remote URL into "owner/repo" for GitHub, leaving
// other URLs as-is (trimmed of a trailing .git).
func normalizeRepo(u string) string {
	if u == "" {
		return ""
	}
	s := strings.TrimSuffix(u, ".git")
	// git@github.com:owner/repo
	if i := strings.Index(s, "github.com:"); i >= 0 {
		return s[i+len("github.com:"):]
	}
	// https://github.com/owner/repo
	if i := strings.Index(s, "github.com/"); i >= 0 {
		return s[i+len("github.com/"):]
	}
	return s
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

	// Python version from the bundled virtualenv (version_info or version).
	if out, err := os.ReadFile(filepath.Join(root, "env", "pyvenv.cfg")); err == nil {
		if m := regexp.MustCompile(`version(?:_info)?\s*=\s*([\d.]+)`).FindStringSubmatch(string(out)); m != nil {
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

// DetectNginxVersion runs `nginx -v` (output like "nginx version: nginx/1.24.0")
// and returns the version string, or "" if it cannot be determined.
func DetectNginxVersion(ctx context.Context, r *runner.Runner) string {
	out, _, _ := r.Exec(ctx, jobs.Step{Command: "nginx", Args: []string{"-v"}}, 30*time.Second)
	if i := strings.Index(out, "nginx/"); i >= 0 {
		return strings.TrimSpace(out[i+len("nginx/"):])
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
