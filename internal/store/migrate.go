package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrations is the embedded migrations directory. It is exported so the CLI
// can register migrations at compile time without filesystem dependencies.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// MigrationFS returns a sub-filesystem rooted at the migrations directory.
// Useful when callers want to enumerate migrations without going through goose.
func MigrationFS() fs.FS {
	sub, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		panic(fmt.Errorf("permafrost: embedded migrations missing: %w", err))
	}
	return sub
}

// Migrator runs goose-based migrations against the configured database.
type Migrator struct {
	url string
}

// NewMigrator creates a Migrator targeting the given database URL.
func NewMigrator(url string) *Migrator {
	return &Migrator{url: url}
}

func (m *Migrator) open() (*sql.DB, error) {
	db, err := sql.Open("pgx", m.url)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return db, nil
}

func (m *Migrator) configure() (*goose.Provider, error) {
	db, err := m.open()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, MigrationFS())
}

// Up applies all pending migrations.
func (m *Migrator) Up(ctx context.Context) error {
	p, err := m.configure()
	if err != nil {
		return err
	}
	defer p.Close()
	_, err = p.Up(ctx)
	return err
}

// Down rolls the most recent migration back.
func (m *Migrator) Down(ctx context.Context) error {
	p, err := m.configure()
	if err != nil {
		return err
	}
	defer p.Close()
	_, err = p.Down(ctx)
	return err
}

// Status prints the current migration state.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	p, err := m.configure()
	if err != nil {
		return nil, err
	}
	defer p.Close()

	statuses, err := p.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationStatus, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, MigrationStatus{
			Version: s.Source.Version,
			Name:    s.Source.Path,
			Applied: s.State == goose.StateApplied,
		})
	}
	return out, nil
}

// MigrationStatus is a renderable summary of one migration's state.
type MigrationStatus struct {
	Version int64
	Name    string
	Applied bool
}

// Ensure pgx stdlib driver is registered (the import is required for side effects).
var _ = stdlib.GetDefaultDriver
