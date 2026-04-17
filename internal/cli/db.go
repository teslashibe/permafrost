package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/store"
)

func newDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database administration",
	}
	cmd.AddCommand(newMigrateCmd())
	return cmd
}

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply or inspect database migrations",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Apply all pending migrations",
			RunE: func(c *cobra.Command, _ []string) error {
				g := FromContext(c.Context())
				if g == nil {
					return errors.New("globals not initialised")
				}
				m := store.NewMigrator(g.Config.Database.URL)
				if err := m.Up(c.Context()); err != nil {
					return fmt.Errorf("migrate up: %w", err)
				}
				g.Log.Info("migrations up: complete")
				return nil
			},
		},
		&cobra.Command{
			Use:   "down",
			Short: "Roll back the most recent migration",
			RunE: func(c *cobra.Command, _ []string) error {
				g := FromContext(c.Context())
				if g == nil {
					return errors.New("globals not initialised")
				}
				m := store.NewMigrator(g.Config.Database.URL)
				if err := m.Down(c.Context()); err != nil {
					return fmt.Errorf("migrate down: %w", err)
				}
				g.Log.Info("migrations down: complete")
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show migration status",
			RunE: func(c *cobra.Command, _ []string) error {
				g := FromContext(c.Context())
				if g == nil {
					return errors.New("globals not initialised")
				}
				m := store.NewMigrator(g.Config.Database.URL)
				statuses, err := m.Status(c.Context())
				if err != nil {
					return fmt.Errorf("migrate status: %w", err)
				}
				for _, s := range statuses {
					state := "pending"
					if s.Applied {
						state = "applied"
					}
					fmt.Printf("%-10d %-10s %s\n", s.Version, state, s.Name)
				}
				return nil
			},
		},
	)
	return cmd
}
