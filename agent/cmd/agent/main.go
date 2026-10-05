// Command agent is the RedCi on-prem agent: it enrolls once with the panel,
// then long-polls for allowlisted jobs and runs them.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	agentcfg "github.com/mwita-lnx/RedCi/agent/internal"
	"github.com/mwita-lnx/RedCi/agent/internal/client"
	"github.com/mwita-lnx/RedCi/agent/internal/journal"
	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	root := &cobra.Command{Use: "ops-agent", Short: "RedCi Ops agent"}
	root.AddCommand(registerCmd(), runCmd(), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	lv := slog.LevelInfo
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the agent version",
		Run:   func(*cobra.Command, []string) { fmt.Println(version) },
	}
}

func registerCmd() *cobra.Command {
	var panelURL, token string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Enroll this server with the panel using a one-time token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := agentcfg.Load()
			if err != nil {
				return err
			}
			if panelURL != "" {
				cfg.PanelURL = panelURL
			}
			if cfg.PanelURL == "" {
				return fmt.Errorf("--panel is required (or set panel_url / OPS_PANEL_URL)")
			}
			if token == "" {
				return fmt.Errorf("--token is required")
			}
			log := newLogger(cfg.LogLevel)
			cli := client.New(cfg.PanelURL, client.WithCAFile(cfg.CAFile))

			resp, err := cli.Register(cmd.Context(), protocol.RegisterRequest{
				EnrollmentToken: token,
				Hostname:        agentcfg.Hostname(),
				AgentVersion:    version,
				Facts:           cfg.CollectFacts(),
			})
			if err != nil {
				return fmt.Errorf("register: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(cfg.CredFile), 0o700); err != nil {
				return err
			}
			if err := cfg.SaveCredential(agentcfg.Credential{ServerID: resp.ServerID, Token: resp.Token}); err != nil {
				return err
			}
			log.Info("enrolled", "server_id", resp.ServerID, "credential", cfg.CredFile)
			fmt.Printf("enrolled as server %d; credential written to %s\n", resp.ServerID, cfg.CredFile)
			fmt.Println("now start the agent:  systemctl enable --now ops-agent  (or: ops-agent run)")
			return nil
		},
	}
	cmd.Flags().StringVar(&panelURL, "panel", "", "panel base URL, e.g. https://panel.internal")
	cmd.Flags().StringVar(&token, "token", "", "one-time enrollment token from the panel")
	return cmd
}

func runCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the agent loop (heartbeat, claim, run, report)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := agentcfg.Load()
			if err != nil {
				return err
			}
			log := newLogger(cfg.LogLevel)
			if cfg.PanelURL == "" {
				return fmt.Errorf("panel_url not set (config.toml or OPS_PANEL_URL)")
			}
			if dryRun {
				log.Info("agent starting in --dry-run mode (commands are logged, not executed)")
			}

			cred, credErr := cfg.LoadCredential()
			if credErr != nil && !dryRun {
				return fmt.Errorf("no credential; run `ops-agent register` first: %w", credErr)
			}

			cli := client.New(cfg.PanelURL, client.WithCAFile(cfg.CAFile), client.WithToken(cred.Token))

			// Journal is best-effort in dry-run with no server.
			jrnlPath := filepath.Join(filepath.Dir(cfg.CredFile), "agent.db")
			jrnl, jErr := journal.Open(jrnlPath)
			if jErr != nil {
				if !dryRun {
					return fmt.Errorf("open journal: %w", jErr)
				}
				log.Warn("journal unavailable in dry-run", "err", jErr)
			}
			if jrnl != nil {
				defer jrnl.Close()
			}

			run := runner.New(runner.Options{DryRun: dryRun, PathEnv: cfg.BenchPathEnv + ":/usr/local/sbin:/usr/local/bin:/usr/bin:/bin"})
			ag := agentcfg.NewAgent(cfg, cred, cli, jrnl, run, version, log)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			log.Info("agent loop started", "panel", cfg.PanelURL, "server_id", cred.ServerID, "version", version)
			return ag.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "log each step's command instead of running it")
	return cmd
}
