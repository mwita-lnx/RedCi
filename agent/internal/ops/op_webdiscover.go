package ops

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DiscoveredWebApp is a web app found under a websites root, reported from files
// alone so the panel can reconcile it without the operator creating it by hand.
// Like bench discovery, nothing on the server changes. A web app is recognised
// by the agent-generated runtime file the deployer always writes:
//   - <root>/<name>/compose.yaml          -> docker runtime
//   - <root>/<name>/ecosystem.config.js   -> pm2 runtime
// The internal port is parsed out of that file; git state is read from the
// checked-out repo's .git (no git subprocess), mirroring bench app discovery.
type DiscoveredWebApp struct {
	Name         string `json:"name"`
	Runtime      string `json:"runtime"` // docker | pm2
	InternalPort int    `json:"internal_port"`
	// Git is the on-disk git state of <root>/<name>/repo (branch/commit/remote),
	// read purely from .git. RemoteURL is normalized to owner/repo for GitHub.
	Git AppGit `json:"git"`
}

// composePortRe matches the host binding the deployer writes into compose.yaml:
//   - "127.0.0.1:<internal>:<container>"
// capturing the internal (host) port.
var composePortRe = regexp.MustCompile(`127\.0\.0\.1:(\d+):\d+`)

// pm2PortRe matches the PORT env the deployer writes into ecosystem.config.js:
//   PORT: "<internal>"
var pm2PortRe = regexp.MustCompile(`PORT:\s*"(\d+)"`)

// DiscoverWebApps walks the given websites roots and reports every web app whose
// agent-generated runtime file is present. It is file-only (no subprocess), so
// it is safe inside the periodic discovery push.
func DiscoverWebApps(roots []string) []DiscoveredWebApp {
	var out []DiscoveredWebApp
	seen := map[string]bool{} // dedupe by name across overlapping roots
	for _, root := range roots {
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			appDir := filepath.Join(root, e.Name())
			app, ok := discoverWebApp(e.Name(), appDir)
			if !ok || seen[app.Name] {
				continue
			}
			seen[app.Name] = true
			out = append(out, app)
		}
	}
	return out
}

// discoverWebApp inspects one <root>/<name> directory using only file reads.
// It returns ok=false when the directory holds no recognisable web-app runtime
// file (so unrelated directories under the websites root are ignored).
func discoverWebApp(name, dir string) (DiscoveredWebApp, bool) {
	app := DiscoveredWebApp{Name: name}

	if port, ok := readComposePort(filepath.Join(dir, "compose.yaml")); ok {
		app.Runtime = "docker"
		app.InternalPort = port
	} else if port, ok := readPM2Port(filepath.Join(dir, "ecosystem.config.js")); ok {
		app.Runtime = "pm2"
		app.InternalPort = port
	} else {
		return DiscoveredWebApp{}, false
	}

	app.Git = readWebAppGit(filepath.Join(dir, "repo"))
	return app, true
}

// readComposePort parses the internal host port out of a generated compose.yaml.
func readComposePort(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	if m := composePortRe.FindStringSubmatch(string(data)); m != nil {
		if p, err := strconv.Atoi(m[1]); err == nil {
			return p, true
		}
	}
	// A compose.yaml exists but we could not parse the port; still a docker app.
	return 0, true
}

// readPM2Port parses the PORT env out of a generated ecosystem.config.js.
func readPM2Port(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	if m := pm2PortRe.FindStringSubmatch(string(data)); m != nil {
		if p, err := strconv.Atoi(m[1]); err == nil {
			return p, true
		}
	}
	return 0, true
}

// readWebAppGit reads the checked-out repo's git state from <repo>/.git using
// only file reads, identical in approach to readBenchAppGit.
func readWebAppGit(repoDir string) AppGit {
	gitDir := filepath.Join(repoDir, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		return AppGit{}
	}
	var g AppGit
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return AppGit{}
	}
	headStr := strings.TrimSpace(string(head))
	if ref := strings.TrimPrefix(headStr, "ref: "); ref != headStr {
		g.Branch = strings.TrimPrefix(ref, "refs/heads/")
		g.Commit = readGitRef(gitDir, ref)
	} else {
		g.Commit = headStr
	}
	g.RemoteURL = readGitRemote(gitDir, g.Branch)
	return g
}
