package main

import (
	"fmt"

	"github.com/spf13/cobra"

	ops "github.com/mwita-lnx/RedCi/ops/internal"
	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/server"
)

// webAppCmd manages web apps from the CLI. Useful for adopting apps that already
// run on a server (so they show up in the UI and can be linked to a route)
// without going through the TOTP-gated web form.
func webAppCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "webapp", Short: "Manage web apps"}

	var (
		serverID                                         int64
		name, runtime, repo, url, branch, health, dfPath string
		internalPort, containerPort                      int
		autoDeploy                                       bool
	)
	add := &cobra.Command{
		Use:   "add",
		Short: "Register a web app (does not deploy; adopts an existing app)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ops.LoadConfig()
			db, authSvc, sealer, err := setup(cfg)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if serverID == 0 {
				return fmt.Errorf("--server is required (server id)")
			}
			if internalPort == 0 {
				return fmt.Errorf("--port (internal_port) is required")
			}
			srv := server.New(cfg, db, authSvc, sealer, events.New(), newLogger())
			id, err := srv.CreateWebApp(cmd.Context(), server.WebAppSpec{
				ServerID:       serverID,
				Name:           name,
				Runtime:        runtime,
				Repo:           repo,
				URL:            url,
				Branch:         branch,
				InternalPort:   internalPort,
				ContainerPort:  containerPort,
				HealthPath:     health,
				DockerfilePath: dfPath,
				AutoDeploy:     autoDeploy,
			})
			if err != nil {
				return err
			}
			fmt.Printf("web app %q registered (id=%d) on server %d, %s :%d\n", name, id, serverID, runtime, internalPort)
			fmt.Println("It is NOT deployed. Create a route for it, then deploy from the UI when ready.")
			return nil
		},
	}
	add.Flags().Int64Var(&serverID, "server", 0, "server id the app runs on")
	add.Flags().StringVar(&name, "name", "", "unique web app name")
	add.Flags().StringVar(&runtime, "runtime", "docker", "runtime: docker or pm2")
	add.Flags().StringVar(&repo, "repo", "", "git repo (owner/name)")
	add.Flags().StringVar(&url, "url", "", "full git clone URL (alternative to --repo)")
	add.Flags().StringVar(&branch, "branch", "main", "git branch")
	add.Flags().IntVar(&internalPort, "port", 0, "internal port the app listens on")
	add.Flags().IntVar(&containerPort, "container-port", 0, "container port (docker; default 3000)")
	add.Flags().StringVar(&health, "health", "/", "health check path")
	add.Flags().StringVar(&dfPath, "dockerfile", "", "Dockerfile path relative to repo root (docker)")
	add.Flags().BoolVar(&autoDeploy, "auto-deploy", false, "enable webhook auto-deploy")
	cmd.AddCommand(add)

	// set-commit: record the commit an app is already running on disk (adopt the
	// live state of an app deployed outside RedCi; does not deploy or restart).
	var appID int64
	var commit string
	setCommit := &cobra.Command{
		Use:   "set-commit",
		Short: "Record the commit a web app is already running (no deploy)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ops.LoadConfig()
			db, authSvc, sealer, err := setup(cfg)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			if appID == 0 || commit == "" {
				return fmt.Errorf("--id and --commit are required")
			}
			srv := server.New(cfg, db, authSvc, sealer, events.New(), newLogger())
			if err := srv.AdoptWebAppCommit(cmd.Context(), appID, commit); err != nil {
				return err
			}
			fmt.Printf("web app %d: current_commit set to %s (status running)\n", appID, commit)
			return nil
		},
	}
	setCommit.Flags().Int64Var(&appID, "id", 0, "web app id")
	setCommit.Flags().StringVar(&commit, "commit", "", "commit SHA the app is running")
	cmd.AddCommand(setCommit)

	// deploy: enqueue a real deploy for a web app (clone/fetch, build, (re)start,
	// health-check) — the CLI equivalent of the UI "Deploy latest" button.
	var depID int64
	var depCommit, depStart, depBuild string
	deploy := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a web app (empty --commit resolves the latest on its branch)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ops.LoadConfig()
			db, authSvc, sealer, err := setup(cfg)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			if depID == 0 {
				return fmt.Errorf("--id is required")
			}
			srv := server.New(cfg, db, authSvc, sealer, events.New(), newLogger())
			id, err := srv.DeployWebApp(cmd.Context(), depID, depCommit, depStart, depBuild, 0)
			if err != nil {
				return err
			}
			fmt.Printf("deploy #%d queued for web app %d\n", id, depID)
			return nil
		},
	}
	deploy.Flags().Int64Var(&depID, "id", 0, "web app id")
	deploy.Flags().StringVar(&depCommit, "commit", "", "commit SHA (empty = latest on branch)")
	deploy.Flags().StringVar(&depStart, "start-command", "", "pm2 start command override")
	deploy.Flags().StringVar(&depBuild, "build-command", "", "pm2 build command override")
	cmd.AddCommand(deploy)
	return cmd
}
