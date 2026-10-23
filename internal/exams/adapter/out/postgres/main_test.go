package postgres

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	pool *pgxpool.Pool
	dsn  string
)

// TestMain starts one Postgres 17 container for the package and applies every
// migration, the studies ones included: exams reference subjects by foreign key.
func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("studies"),
		tcpostgres.WithUsername("studies"),
		tcpostgres.WithPassword("studies"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
		// Same ICU default collation as the studies suite: the queries must pin
		// COLLATE "C" themselves for a deterministic order.
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_INITDB_ARGS": "--locale-provider=icu --icu-locale=en-US",
		}),
	)
	if err != nil {
		panic(err)
	}
	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(err)
	}
	if err := migrateUp(); err != nil {
		panic(err)
	}
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	pool.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func newMigrate() (*migrate.Migrate, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return migrate.New("file://"+abs, "pgx5://"+strings.TrimPrefix(dsn, "postgres://"))
}

func migrateUp() error {
	m, err := newMigrate()
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
