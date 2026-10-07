package jobs

import (
	"encoding/base64"
)

// gitTokenEnv builds the environment that feeds a short-lived clone token to
// git without it ever appearing on the command line (so it is not visible in
// `ps` or logs). Returns nil when no token is set.
func gitTokenEnv(token string) []string {
	if token == "" {
		return nil
	}
	auth := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=" + auth,
	}
}

// InstallAppParams.Steps: install an existing app on a site, then ping.
func (p InstallAppParams) Steps(ctx PlanContext) ([]Step, error) {
	return []Step{
		{
			Name: "install app", Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", p.Site, "install-app", p.App},
		},
	}, nil
}

// GetFrappeAppParams.Steps: fetch a custom app onto a bench for the first time.
func (p GetFrappeAppParams) Steps(ctx PlanContext) ([]Step, error) {
	token := ctx.secret(p.CloneTok)
	return []Step{
		{
			Name: "get app", Dir: p.BenchPath, Command: "bench",
			Args: []string{"get-app", "--branch", p.Branch, "https://github.com/" + p.Repo},
			Env:  gitTokenEnv(token), Redact: []string{token},
		},
	}, nil
}

// NOTE: deploy_frappe_app and rollback_frappe_app run as custom ops (see
// agent/internal/ops/op_frappe.go) so they can capture the previous commit and
// the pre-deploy database backups into result_json for rollback.

// BackupSiteParams.Steps: a single bench backup, optionally with files.
func (p BackupSiteParams) Steps(ctx PlanContext) ([]Step, error) {
	args := []string{"--site", p.Site, "backup"}
	if p.WithFiles {
		args = append(args, "--with-files")
	}
	return []Step{
		{Name: "backup", Dir: p.BenchPath, Command: "bench", Args: args},
	}, nil
}
