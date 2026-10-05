package jobs

import "fmt"

// GetFrappeAppParams drives `get_frappe_app` (first install of a custom app on
// a bench). The clone token rides in git's environment, never in Args.
type GetFrappeAppParams struct {
	BenchPath string    `json:"bench_path"`
	Repo      string    `json:"repo"`   // owner/name
	Branch    string    `json:"branch"`
	CloneTok  SecretRef `json:"clone_token"`
}

func (p GetFrappeAppParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	if err := validateRepo(p.Repo); err != nil {
		return err
	}
	return validateBranch(p.Branch)
}

// DeployFrappeAppParams drives `deploy_frappe_app` on one bench. Sites is the
// list of sites on this bench that have the app installed (migrated in order).
type DeployFrappeAppParams struct {
	BenchPath   string    `json:"bench_path"`
	App         string    `json:"app"`
	Branch      string    `json:"branch"`
	Commit      string    `json:"commit"`
	Sites       []string  `json:"sites"`
	HasPackage  bool      `json:"has_package_json"` // run yarn install
	Maintenance bool      `json:"maintenance"`      // set-maintenance-mode around migrate
	CloneTok    SecretRef `json:"clone_token"`
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

// WriteProxyRouteParams drives `write_proxy_route`.
type WriteProxyRouteParams struct {
	RouteID       int64  `json:"route_id"`
	Domain        string `json:"domain"`
	Upstream      string `json:"upstream"`
	SSL           bool   `json:"ssl"`
	Snippet       string `json:"snippet"`        // admin-only, inserted verbatim
	AdminOverride bool   `json:"admin_override"` // allow a non-localhost upstream
}

func (p WriteProxyRouteParams) Validate([]string) error {
	if err := ValidateDomain(p.Domain); err != nil {
		return err
	}
	return ValidateUpstream(p.Upstream, p.AdminOverride)
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

// DeployWebAppParams drives `deploy_web_app`.
type DeployWebAppParams struct {
	Name          string     `json:"name"`
	Repo          string     `json:"repo"`
	Branch        string     `json:"branch"`
	Commit        string     `json:"commit"`
	InternalPort  int        `json:"internal_port"`
	ContainerPort int        `json:"container_port"`
	HealthPath    string     `json:"health_path"`
	Env           SecretRef  `json:"env"`
	BuildArgs     []BuildArg `json:"build_args"`
	CloneTok      SecretRef  `json:"clone_token"`
}

func (p DeployWebAppParams) Validate([]string) error {
	if err := ValidateName(p.Name); err != nil {
		return err
	}
	if err := validateRepo(p.Repo); err != nil {
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
	Name          string `json:"name"`
	Commit        string `json:"commit"` // earlier image tag to restore
	InternalPort  int    `json:"internal_port"`
	ContainerPort int    `json:"container_port"`
	HealthPath    string `json:"health_path"`
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
