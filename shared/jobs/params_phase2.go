package jobs

import (
	"fmt"
	"strings"
)

// GetFrappeAppParams drives `get_frappe_app` (first install of a custom app on
// a bench). The clone token rides in git's environment, never in Args.
type GetFrappeAppParams struct {
	BenchPath string    `json:"bench_path"`
	Repo      string    `json:"repo"` // owner/name (GitHub shorthand); ignored when URL is set
	URL       string    `json:"url"`  // full git clone URL; takes precedence over Repo
	Branch    string    `json:"branch"`
	CloneTok  SecretRef `json:"clone_token"`
}

func (p GetFrappeAppParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	// Either a full clone URL or an owner/name repo shorthand is required.
	if p.URL != "" {
		if err := ValidateGitURL(p.URL); err != nil {
			return err
		}
	} else if err := validateRepo(p.Repo); err != nil {
		return err
	}
	return validateBranch(p.Branch)
}

// DeployFrappeAppParams drives `deploy_frappe_app` on one bench. Sites is the
// list of sites on this bench that have the app installed (migrated in order).
type DeployFrappeAppParams struct {
	BenchPath    string    `json:"bench_path"`
	App          string    `json:"app"`
	Branch       string    `json:"branch"`
	Commit       string    `json:"commit"`
	Sites        []string  `json:"sites"`
	HasPackage   bool      `json:"has_package_json"`    // run yarn install
	Maintenance  bool      `json:"maintenance"`         // set-maintenance-mode around migrate
	SkipPatches  bool      `json:"skip_failing_patches"` // bench migrate --skip-failing
	CloneTok     SecretRef `json:"clone_token"`
}

func (p DeployFrappeAppParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	if err := ValidateAppName(p.App); err != nil {
		return err
	}
	if err := ValidateCommit(p.Commit); err != nil {
		return err
	}
	if err := validateBranch(p.Branch); err != nil {
		return err
	}
	if len(p.Sites) == 0 {
		return fmt.Errorf("deploy_frappe_app: no sites to migrate")
	}
	for _, s := range p.Sites {
		if err := ValidateDomain(s); err != nil {
			return err
		}
	}
	return nil
}

// RollbackFrappeAppParams drives `rollback_frappe_app`: check out the previous
// commit, reinstall deps, and for each site restore the pre-deploy database
// backup taken during the deploy we are reverting. Backups maps site -> the
// absolute path of the database backup file to restore.
type RollbackFrappeAppParams struct {
	BenchPath  string            `json:"bench_path"`
	App        string            `json:"app"`
	Branch     string            `json:"branch"`
	Commit     string            `json:"commit"`  // previous (rollback target) commit
	Sites      []string          `json:"sites"`
	Backups    map[string]string `json:"backups"` // site -> db backup file path
	HasPackage bool              `json:"has_package_json"`
	CloneTok   SecretRef         `json:"clone_token"`
}

func (p RollbackFrappeAppParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	if err := ValidateAppName(p.App); err != nil {
		return err
	}
	if err := ValidateCommit(p.Commit); err != nil {
		return err
	}
	if err := validateBranch(p.Branch); err != nil {
		return err
	}
	if len(p.Sites) == 0 {
		return fmt.Errorf("rollback_frappe_app: no sites to restore")
	}
	for _, s := range p.Sites {
		if err := ValidateDomain(s); err != nil {
			return err
		}
	}
	return nil
}

// BackupSiteParams drives `backup_site`.
type BackupSiteParams struct {
	BenchPath string `json:"bench_path"`
	Site      string `json:"site"`
	WithFiles bool   `json:"with_files"`
}

func (p BackupSiteParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	return ValidateDomain(p.Site)
}

// RouteAction is what a RouteLocation does with a matched request.
type RouteAction string

const (
	RouteActionProxy      RouteAction = "proxy"      // proxy_pass to Target (an upstream URL)
	RouteActionFilesystem RouteAction = "filesystem" // serve from disk: root Target; try_files
	RouteActionRedirect   RouteAction = "redirect"   // return 301 to Target
)

// RouteMatch is how a location path is matched (nginx location modifier).
type RouteMatch string

const (
	RouteMatchPrefix RouteMatch = "prefix" // location <path>
	RouteMatchExact  RouteMatch = "exact"  // location = <path>
	RouteMatchRegex  RouteMatch = "regex"  // location ~ <path>
)

// RouteLocation is one nginx location rule within a vhost. This is what lets a
// single domain path-split between a Next.js frontend and a Frappe backend
// (the report's "Model B"): e.g. /api/ -> proxy :8000, /assets -> filesystem,
// /socket.io/ -> proxy :9000 (websocket), / -> proxy :3000.
type RouteLocation struct {
	Path      string      `json:"path"`              // e.g. "/api/", "/", "/website_script.js"
	Match     RouteMatch  `json:"match,omitempty"`   // default prefix
	Action    RouteAction `json:"action"`            // proxy | filesystem | redirect
	Target    string      `json:"target"`            // upstream URL | filesystem root | redirect URL
	Websocket bool        `json:"websocket"`          // emit Upgrade/Connection only here
	FrappeProxy bool      `json:"frappe_proxy"`      // add X-Frappe-Site-Name + Frappe proxy headers
}

// WriteProxyRouteParams drives `write_proxy_route`.
//
// Two shapes, selected by whether Locations is set:
//   - Simple (Locations empty): one catch-all `location /` → Upstream. Backward
//     compatible with every existing route.
//   - Model B (Locations set): a full path-split vhost. FrappeSite feeds
//     X-Frappe-Site-Name on frappe_proxy locations; StaticRoot is the default
//     filesystem root for filesystem locations that don't set their own Target.
type WriteProxyRouteParams struct {
	RouteID       int64           `json:"route_id"`
	Domain        string          `json:"domain"`
	Upstream      string          `json:"upstream"`                 // simple mode: the single catch-all upstream
	SSL           bool            `json:"ssl"`
	Snippet       string          `json:"snippet"`                  // admin-only, inserted verbatim
	AdminOverride bool            `json:"admin_override"`           // allow a non-localhost upstream
	Aliases       []string        `json:"aliases,omitempty"`        // extra server_name hostnames
	FrappeSite    string          `json:"frappe_site,omitempty"`    // X-Frappe-Site-Name for frappe_proxy locations
	StaticRoot    string          `json:"static_root,omitempty"`    // default root for filesystem locations
	Locations     []RouteLocation `json:"locations,omitempty"`      // Model B path rules; empty = simple mode
	RawConfig     string          `json:"raw_config,omitempty"`     // raw mode: full verbatim vhost; when set, written as-is (no template)
}

func (p WriteProxyRouteParams) Validate([]string) error {
	if err := ValidateDomain(p.Domain); err != nil {
		return err
	}
	for _, a := range p.Aliases {
		if err := ValidateDomain(a); err != nil {
			return fmt.Errorf("alias %q: %w", a, err)
		}
	}
	// Raw mode: the operator supplies the whole vhost; nothing to template, and
	// the agent validates it with `nginx -t` before reload. Only sanity-check it
	// is non-trivial and mentions a server block.
	if p.RawConfig != "" {
		if !strings.Contains(p.RawConfig, "server") {
			return fmt.Errorf("raw config must contain an nginx server block")
		}
		return nil
	}
	if len(p.Locations) == 0 {
		// Simple mode: one upstream required.
		return ValidateUpstream(p.Upstream, p.AdminOverride)
	}
	// Model B: validate each proxy location's upstream; filesystem/redirect
	// targets are free-form (filesystem root validated by the agent against
	// allowlisted roots at apply time is out of scope — these are admin-set).
	for i, loc := range p.Locations {
		if loc.Path == "" {
			return fmt.Errorf("location %d: path is required", i)
		}
		switch loc.Action {
		case RouteActionProxy:
			if err := ValidateUpstream(loc.Target, p.AdminOverride); err != nil {
				return fmt.Errorf("location %q: %w", loc.Path, err)
			}
		case RouteActionFilesystem, RouteActionRedirect:
			if loc.Target == "" && loc.Action == RouteActionRedirect {
				return fmt.Errorf("location %q: redirect needs a target", loc.Path)
			}
		default:
			return fmt.Errorf("location %q: action must be proxy|filesystem|redirect", loc.Path)
		}
	}
	return nil
}

// DeleteProxyRouteParams drives `delete_proxy_route`.
type DeleteProxyRouteParams struct {
	Domain string `json:"domain"`
}

func (p DeleteProxyRouteParams) Validate([]string) error {
	return ValidateDomain(p.Domain)
}

// BuildArg is a build-time variable for a web-app image (NEXT_PUBLIC_*).
type BuildArg struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// WebRuntime selects how a web app runs on the server.
type WebRuntime string

const (
	WebRuntimeDocker WebRuntime = "docker" // docker build + compose up
	WebRuntimePM2    WebRuntime = "pm2"    // npm/yarn build + pm2 start/reload
)

// DeployWebAppParams drives `deploy_web_app`.
type DeployWebAppParams struct {
	Name          string     `json:"name"`
	Runtime       WebRuntime `json:"runtime"` // docker (default) | pm2
	Repo          string     `json:"repo"`    // owner/name (GitHub shorthand); ignored when URL is set
	URL           string     `json:"url"`     // full git clone URL; takes precedence over Repo
	Branch        string     `json:"branch"`
	Commit        string     `json:"commit"`
	InternalPort   int        `json:"internal_port"`
	ContainerPort  int        `json:"container_port"`
	HealthPath     string     `json:"health_path"`
	WebsitesRoot   string     `json:"websites_root"`   // agent websites root; app lives at <root>/<name>
	DockerfilePath string     `json:"dockerfile_path"` // docker: path to Dockerfile relative to repo root ("" = repo-root Dockerfile)
	StartCommand   string     `json:"start_command"`   // pm2: command to run (default: npm start)
	BuildCommand   string     `json:"build_command"`   // pm2: build step (default: npm run build; "" to skip)
	Env           SecretRef  `json:"env"`
	BuildArgs     []BuildArg `json:"build_args"`
	CloneTok      SecretRef  `json:"clone_token"`
}

func (p DeployWebAppParams) Validate([]string) error {
	if err := ValidateName(p.Name); err != nil {
		return err
	}
	if p.Runtime != "" && p.Runtime != WebRuntimeDocker && p.Runtime != WebRuntimePM2 {
		return fmt.Errorf("runtime %q: must be docker or pm2", p.Runtime)
	}
	// A full clone URL or an owner/name repo is required.
	if p.URL != "" {
		if err := ValidateGitURL(p.URL); err != nil {
			return err
		}
	} else if err := validateRepo(p.Repo); err != nil {
		return err
	}
	if err := ValidateCommit(p.Commit); err != nil {
		return err
	}
	if err := ValidatePort(p.InternalPort); err != nil {
		return err
	}
	if p.ContainerPort != 0 {
		if err := ValidatePort(p.ContainerPort); err != nil {
			return err
		}
	}
	return nil
}

// RollbackWebAppParams drives `rollback_web_app`.
type RollbackWebAppParams struct {
	Name          string     `json:"name"`
	Runtime       WebRuntime `json:"runtime"`
	Commit        string     `json:"commit"` // earlier image tag / commit to restore
	InternalPort  int        `json:"internal_port"`
	ContainerPort int        `json:"container_port"`
	HealthPath    string     `json:"health_path"`
	WebsitesRoot  string     `json:"websites_root"`
	StartCommand  string     `json:"start_command"`
	BuildCommand  string     `json:"build_command"`
}

func (p RollbackWebAppParams) Validate([]string) error {
	if err := ValidateName(p.Name); err != nil {
		return err
	}
	if err := ValidateCommit(p.Commit); err != nil {
		return err
	}
	return ValidatePort(p.InternalPort)
}
