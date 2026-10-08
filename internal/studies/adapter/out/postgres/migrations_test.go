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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	pool *pgxpool.Pool
	dsn  string
)

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
		// An ICU default collation orders punctuation and control bytes
		// differently from byte order, so ordering tests fail unless the
		// queries pin COLLATE "C" themselves (production runs musl/alpine,
		// whose default happens to match byte order and would hide that).
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
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
}

func exec(t *testing.T, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// guardedTable creates a throwaway table wired to forbid_hard_delete(), the
// same way every Spec 16 table will be from Slice 02 on.
func guardedTable(t *testing.T, name string) {
	t.Helper()
	exec(t, `CREATE TABLE `+name+` (id int PRIMARY KEY)`)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS `+name) })
	exec(t, `CREATE TRIGGER `+name+`_no_delete BEFORE DELETE ON `+name+`
	         FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete()`)
	exec(t, `INSERT INTO `+name+` (id) VALUES (1), (2)`)
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestForbidHardDeleteRejectsDelete(t *testing.T) {
	skipShort(t)
	guardedTable(t, "guard_delete")

	_, err := pool.Exec(context.Background(), `DELETE FROM guard_delete WHERE id = 1`)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("DELETE error = %v, want a PgError", err)
	}
	if pgErr.Code != "23001" {
		t.Errorf("SQLSTATE = %s, want 23001 (restrict_violation)", pgErr.Code)
	}
	if !strings.Contains(pgErr.Message, "guard_delete") {
		t.Errorf("message = %q, want it to name the table", pgErr.Message)
	}
	if n := countRows(t, "guard_delete"); n != 2 {
		t.Errorf("rows after rejected DELETE = %d, want 2", n)
	}
}

func TestForbidHardDeleteAllowsTruncate(t *testing.T) {
	skipShort(t)
	guardedTable(t, "guard_truncate")

	exec(t, `TRUNCATE guard_truncate`)

	if n := countRows(t, "guard_truncate"); n != 0 {
		t.Errorf("rows after TRUNCATE = %d, want 0", n)
	}
}

func TestMigrateDownRemovesFunction(t *testing.T) {
	skipShort(t)
	functionExists := func() bool {
		var n int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_proc WHERE proname = 'forbid_hard_delete'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if !functionExists() {
		t.Fatal("forbid_hard_delete() missing after migrate up")
	}

	m, err := newMigrate()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if functionExists() {
		t.Error("forbid_hard_delete() still present after migrate down")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if !functionExists() {
		t.Error("forbid_hard_delete() missing after re-applying migrations")
	}
}
