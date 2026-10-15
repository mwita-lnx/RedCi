package jobs

import (
	"fmt"
	"path"
	"strings"
)

// WebAppPaths are the on-server locations for a web app, derived from the
// websites root and the app name. Each app lives at <websites_root>/<name>/.
type WebAppPaths struct {
	Root      string // <websites_root>/<name>
	Repo      string // <root>/repo
	Compose   string // <root>/compose.yaml      (docker runtime)
	Ecosystem string // <root>/ecosystem.config.js (pm2 runtime)
	EnvFile   string // <root>/.env
}

// Paths returns the standard layout for a web app under websitesRoot.
func WebAppPathsFor(websitesRoot, name string) WebAppPaths {
	root := path.Join(websitesRoot, name)
	return WebAppPaths{
		Root:      root,
		Repo:      path.Join(root, "repo"),
		Compose:   path.Join(root, "compose.yaml"),
		Ecosystem: path.Join(root, "ecosystem.config.js"),
		EnvFile:   path.Join(root, ".env"),
	}
}

// cloneURL returns the git clone URL for a web app deploy: a full URL when set,
// else the GitHub owner/name shorthand.
func (p DeployWebAppParams) cloneURL() string {
	if p.URL != "" {
		return p.URL
	}
	return "https://github.com/" + p.Repo
}

// ImageTag is the docker image tag for a commit (first 12 hex chars).
func ImageTag(name, commit string) string {
	sha := commit
	if len(sha) > 12 {
		sha = sha[:12]
	}
	return fmt.Sprintf("ops/%s:%s", name, sha)
}

// FetchSteps returns the git fetch/checkout steps for a web app deploy. first
// indicates a fresh clone vs an update of an existing checkout.
func (p DeployWebAppParams) FetchSteps(ctx PlanContext, paths WebAppPaths, first bool) []Step {
	token := ctx.secret(p.CloneTok)
	env := gitTokenEnv(token)
	if first {
		return []Step{{
			Name: "clone", Command: "git",
			Args:   []string{"clone", "--filter=blob:none", p.cloneURL(), paths.Repo},
			Env:    env, Redact: []string{token},
		}}
	}
	return []Step{
		{Name: "fetch", Dir: paths.Repo, Command: "git",
			Args: []string{"fetch", "origin", p.Commit}, Env: env, Redact: []string{token}},
		{Name: "checkout", Dir: paths.Repo, Command: "git",
			Args: []string{"checkout", "--force", p.Commit}},
	}
}

// PM2BuildSteps returns the node install + optional build steps for a pm2 app.
// install uses `npm ci` when a lockfile is expected; build runs the configured
// build command (default `npm run build`), skipped when BuildCommand is "-".
func (p DeployWebAppParams) PM2BuildSteps(paths WebAppPaths) []Step {
	steps := []Step{
		{Name: "npm install", Dir: paths.Repo, Command: "npm", Args: []string{"install"}},
	}
	build := p.BuildCommand
	if build == "" {
		build = "npm run build"
	}
	if build != "-" {
		steps = append(steps, Step{
			Name: "build", Dir: paths.Repo, Command: "sh", Args: []string{"-c", build},
		})
	}
	return steps
}

// PM2StartStep (re)starts the app under pm2 using the generated ecosystem file.
// `pm2 startOrReload` starts if absent, zero-downtime reloads if already running.
func (p DeployWebAppParams) PM2StartStep(paths WebAppPaths) Step {
	return Step{
		Name: "pm2 start", Dir: paths.Repo, Command: "pm2",
		Args: []string{"startOrReload", paths.Ecosystem, "--update-env"},
	}
}

// PM2EcosystemFile renders the pm2 ecosystem config. The app runs the start
// command, binds PORT to the internal port, and loads .env. cwd is the repo.
func PM2EcosystemFile(name, repoDir, startCommand string, internalPort int, envFile string) string {
	if startCommand == "" {
		startCommand = "npm start"
	}
	return fmt.Sprintf(`module.exports = {
  apps: [{
    name: %q,
    cwd: %q,
    script: "sh",
    args: ["-c", %q],
    env: { PORT: "%d", NODE_ENV: "production", HOST: "127.0.0.1" },
    env_file: %q,
    autorestart: true,
    max_restarts: 10,
    time: true,
  }],
};
`, name, repoDir, startCommand, internalPort, envFile)
}

// BuildStep returns the docker build step for the deploy's commit.
func (p DeployWebAppParams) BuildStep(paths WebAppPaths) Step {
	args := []string{
		"build", "-t", ImageTag(p.Name, p.Commit),
		"--label", "ops.commit=" + p.Commit,
	}
	// Point at a specific Dockerfile when set (e.g. monorepo apps/web/Dockerfile).
	// The context stays the repo root so build-time COPY paths are unchanged.
	if df := p.DockerfileRel(); df != "" {
		args = append(args, "-f", path.Join(paths.Repo, df))
	}
	for _, ba := range p.BuildArgs {
		args = append(args, "--build-arg", ba.Key+"="+ba.Value)
	}
	args = append(args, paths.Repo)
	return Step{Name: "build image", Dir: paths.Repo, Command: "docker", Args: args}
}

// DockerfileRel returns a cleaned, repo-relative Dockerfile path, or "" for the
// default repo-root Dockerfile. It strips a leading "./" and rejects anything
// that escapes the repo (absolute paths or "..") by returning "" (falls back to
// the default), so the value is always safe to join onto the repo dir.
func (p DeployWebAppParams) DockerfileRel() string {
	df := strings.TrimSpace(p.DockerfilePath)
	if df == "" || df == "Dockerfile" || df == "./Dockerfile" {
		return ""
	}
	df = path.Clean(df)
	if path.IsAbs(df) || df == ".." || strings.HasPrefix(df, "../") {
		return ""
	}
	return df
}

// ComposeUpStep brings the app up with the given image tag.
func composeUpStep(name string, paths WebAppPaths, tag string) Step {
	return Step{
		Name: "compose up", Dir: paths.Root, Command: "docker",
		Args: []string{"compose", "-p", name, "-f", paths.Compose, "up", "-d", "--remove-orphans"},
		Env:  []string{"IMAGE_TAG=" + tagVersion(tag)},
	}
}

// tagVersion returns just the version portion of an ops/<name>:<ver> tag.
func tagVersion(tag string) string {
	for i := len(tag) - 1; i >= 0; i-- {
		if tag[i] == ':' {
			return tag[i+1:]
		}
	}
	return tag
}

// ComposeFile renders the agent-generated compose.yaml for a web app. The agent
// always generates this (never the repo) so every app binds to localhost and
// restarts on failure.
func ComposeFile(name string, internalPort, containerPort int) string {
	if containerPort == 0 {
		containerPort = 3000
	}
	return fmt.Sprintf(`name: %s
services:
  web:
    image: ops/%s:${IMAGE_TAG}
    restart: unless-stopped
    env_file: .env
    ports:
      - "127.0.0.1:%d:%d"
    logging:
      driver: local
      options: { max-size: "10m", max-file: "3" }
`, name, name, internalPort, containerPort)
}
