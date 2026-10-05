// Command panel is the RedCi Ops Panel control-plane binary.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	ops "github.com/mwita-lnx/RedCi/ops/internal"
	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/secrets"
	"github.com/mwita-lnx/RedCi/ops/internal/server"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/internal/worker"
)

func main() {
	root := &cobra.Command{
		Use:   "panel",
		Short: "RedCi Ops Panel",
	}
	root.AddCommand(serveCmd(), migrateCmd(), userCmd(), serverCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// setup opens the DB, loads the master key, and builds the auth service. It is
// shared by every subcommand.
func setup(cfg ops.Config) (*store.DB, *auth.Service, *secrets.Sealer, error) {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, nil, err
	}
	key, err := secrets.LoadMasterKey(cfg.MasterKeyPath)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	return db, auth.NewService(db, sealer), sealer, nil
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ops.LoadConfig()
			log := newLogger()

			db, authSvc, sealer, err := setup(cfg)
			if err != nil {
				return err
			}
			defer db.Close()

			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}

			bus := events.New()
			srv := server.New(cfg, db, authSvc, sealer, bus, log)
			wrk := worker.New(db, bus, srv, log)
			wrk.SetAlerter(worker.NewAlerter(cfg.AlertWebhook))
			srv.SetNudger(wrk) // webhook/UI actions kick the worker immediately

			httpSrv := &http.Server{
				Addr:              cfg.Listen,
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Worker runs in its own goroutine, sharing the shutdown context.
			go func() {
				if err := wrk.Run(ctx); err != nil {
					log.Error("worker stopped", "err", err)
				}
			}()

			go func() {
				log.Info("panel listening", "addr", cfg.Listen)
				if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("server error", "err", err)
					stop()
				}
			}()

			<-ctx.Done()
			log.Info("shutting down")
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return httpSrv.Shutdown(shutCtx)
		},
	}
}

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations and exit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ops.LoadConfig()
			db, err := store.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("migrations applied")
			return nil
		},
	}
}

func userCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage users"}

	var email, name, roleFlag, passwordFlag string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a user (prompts for a password unless --password is given)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ops.LoadConfig()
			db, authSvc, _, err := setup(cfg)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			if email == "" {
				return fmt.Errorf("--email is required")
			}
			role := auth.Role(roleFlag)
			if !role.Valid() {
				return fmt.Errorf("--role must be admin, deployer or viewer")
			}
			if name == "" {
				name = email
			}
			var pw string
			if passwordFlag != "" {
				if len(passwordFlag) < 8 {
					return fmt.Errorf("password must be at least 8 characters")
				}
				pw = passwordFlag
			} else {
				pw, err = promptPassword()
				if err != nil {
					return err
				}
			}
			u, err := authSvc.CreateUser(cmd.Context(), email, name, pw, role)
			if err != nil {
				return err
			}
			fmt.Printf("created user %s (id=%d, role=%s)\n", u.Email, u.ID, u.Role)
			fmt.Println("They will enroll in TOTP on first login.")
			return nil
		},
	}
	create.Flags().StringVar(&email, "email", "", "user email")
	create.Flags().StringVar(&name, "name", "", "display name (defaults to email)")
	create.Flags().StringVar(&roleFlag, "role", "admin", "role: admin, deployer or viewer")
	create.Flags().StringVar(&passwordFlag, "password", "", "password (skips interactive prompt; useful in CI/Docker)")
	cmd.AddCommand(create)
	return cmd
}

func promptPassword() (string, error) {
	fmt.Print("Password: ")
	b1, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Confirm:  ")
	b2, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(b1) != string(b2) {
		return "", fmt.Errorf("passwords do not match")
	}
	if len(b1) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	return string(b1), nil
}
