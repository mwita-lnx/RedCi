package jobs

import (
	"fmt"
	"path"
)

// WebAppPaths are the on-server locations for a web app, derived from the
// apps root and the app name.
type WebAppPaths struct {
	Root    string // /srv/ops/apps/<name>
	Repo    string // <root>/repo
	Compose string // <root>/compose.yaml
	EnvFile string // <root>/.env
}

// Paths returns the standard layout for a web app under appsRoot.
func WebAppPathsFor(appsRoot, name string) WebAppPaths {
	root := path.Join(appsRoot, name)
	return WebAppPaths{
		Root:    root,
		Repo:    path.Join(root, "repo"),
		Compose: path.Join(root, "compose.yaml"),
		EnvFile: path.Join(root, ".env"),
	}
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
			Args:   []string{"clone", "--filter=blob:none", "https://github.com/" + p.Repo, paths.Repo},
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

// BuildStep returns the docker build step for the deploy's commit.
func (p DeployWebAppParams) BuildStep(paths WebAppPaths) Step {
	args := []string{
		"build", "-t", ImageTag(p.Name, p.Commit),
		"--label", "ops.commit=" + p.Commit,
	}
	for _, ba := range p.BuildArgs {
		args = append(args, "--build-arg", ba.Key+"="+ba.Value)
	}
	args = append(args, paths.Repo)
	return Step{Name: "build image", Dir: paths.Repo, Command: "docker", Args: args}
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
