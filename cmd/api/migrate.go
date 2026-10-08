package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const migrationsDir = "migrations"

func runMigrations(databaseURL string) error {
	ups, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		return err
	}
	if len(ups) == 0 {
		slog.Info("no migrations to run")
		return nil
	}

	abs, err := filepath.Abs(migrationsDir)
	if err != nil {
		return err
	}
	m, err := migrate.New("file://"+abs, pgxURL(databaseURL))
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	slog.Info("migrations applied", "count", len(ups))
	return nil
}

// golang-migrate selects its driver from the URL scheme; the pgx/v5 driver
// registers as "pgx5" while compose supplies a standard postgres:// URL.
func pgxURL(databaseURL string) string {
	if rest, ok := strings.CutPrefix(databaseURL, "postgres://"); ok {
		return "pgx5://" + rest
	}
	if rest, ok := strings.CutPrefix(databaseURL, "postgresql://"); ok {
		return "pgx5://" + rest
	}
	return databaseURL
}

func init() {
	// Keep relative migration lookup working no matter where the binary starts,
	// by anchoring to the executable's directory when migrations/ is not in CWD.
	if _, err := os.Stat(migrationsDir); err == nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	candidate := filepath.Join(filepath.Dir(exe), migrationsDir)
	if _, err := os.Stat(candidate); err == nil {
		_ = os.Chdir(filepath.Dir(exe))
	}
}
