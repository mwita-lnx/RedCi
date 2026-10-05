package main

import (
	"fmt"

	"github.com/spf13/cobra"

	ops "github.com/mwita-lnx/RedCi/ops/internal"
	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/server"
)

// serverCmd manages servers from the CLI until the Servers UI lands in Phase 2.
func serverCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "server", Short: "Manage on-prem servers"}

	var name, hostname string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a server and print its one-time enrollment command",
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
			if hostname == "" {
				hostname = name
			}
			srv := server.New(cfg, db, authSvc, sealer, events.New(), newLogger())
			id, token, err := srv.CreateEnrollment(cmd.Context(), name, hostname)
			if err != nil {
				return err
			}
			fmt.Printf("Server %q added (id=%d). Enroll it within 1 hour:\n\n", name, id)
			fmt.Printf("  sudo -u frappe ops-agent register --panel %s --token %s\n\n", cfg.BaseURL, token)
			fmt.Println("This token is shown once.")
			return nil
		},
	}
	add.Flags().StringVar(&name, "name", "", "server name")
	add.Flags().StringVar(&hostname, "hostname", "", "hostname (defaults to name)")
	cmd.AddCommand(add)
	return cmd
}
