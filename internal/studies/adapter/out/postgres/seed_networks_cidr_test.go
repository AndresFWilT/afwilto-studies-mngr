package postgres

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

// Spec 17 FR-10 — migration 0008 seeds TI → Networks → CIDR with one note.
const (
	seedTI      = "00000000-0000-7000-8017-000000000001"
	seedNetwork = "00000000-0000-7000-8017-000000000002"
	seedSubject = "00000000-0000-7000-8017-000000000003"
	seedNote    = "00000000-0000-7000-8017-000000000004"
	seedTitle   = "IPv4 and IPv6 CIDR — guide and tables"
	seedVersion = 8
)

// migrateTo moves the schema to version v and restores the latest version
// when the test ends, so later tests always see the full schema.
func migrateTo(t *testing.T, v uint) *migrate.Migrate {
	t.Helper()
	m, err := newMigrate()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(v); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to %d: %v", v, err)
	}
	t.Cleanup(func() {
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			t.Errorf("restore latest migration: %v", err)
		}
		m.Close()
	})
	return m
}

func queryString(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return s
}

func queryInt(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// freshUpTo re-applies every migration on an empty schema, up to version v.
func freshUpTo(t *testing.T, v uint) *migrate.Migrate {
	t.Helper()
	m := migrateTo(t, 1)
	if err := m.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := m.Migrate(v); err != nil {
		t.Fatalf("migrate to %d: %v", v, err)
	}
	return m
}

func TestSeedNetworksCidrOnFreshDatabase(t *testing.T) {
	skipShort(t)
	freshUpTo(t, seedVersion)

	// AC-S1: TI (root) → Networks (depth 2) → subject CIDR → one note.
	if got := queryString(t, `SELECT name FROM categories WHERE id = $1 AND parent_id IS NULL AND deleted_at IS NULL`, seedTI); got != "TI" {
		t.Errorf("root = %q, want TI", got)
	}
	if got := queryString(t, `SELECT name || ' ' || color FROM categories WHERE id = $1 AND parent_id = $2 AND deleted_at IS NULL`, seedNetwork, seedTI); got != "Networks #0EA5E9" {
		t.Errorf("child = %q, want Networks #0EA5E9", got)
	}
	if got := queryString(t, `SELECT name FROM subjects WHERE id = $1 AND category_id = $2 AND deleted_at IS NULL`, seedSubject, seedNetwork); got != "CIDR" {
		t.Errorf("subject = %q, want CIDR", got)
	}
	if got := queryString(t, `SELECT k.name FROM notes n JOIN note_kinds k ON k.id = n.kind_id WHERE n.id = $1 AND n.subject_id = $2 AND n.title = $3 AND n.deleted_at IS NULL`, seedNote, seedSubject, seedTitle); got != "Summary" {
		t.Errorf("note kind = %q, want Summary", got)
	}
	tags := queryString(t, `SELECT string_agg(t.name, ',' ORDER BY t.name) FROM note_tags nt JOIN tags t ON t.id = nt.tag_id WHERE nt.note_id = $1 AND nt.deleted_at IS NULL`, seedNote)
	if tags != "cidr,ipv4,ipv6,networking,subnetting" {
		t.Errorf("tags = %q", tags)
	}
	links := queryString(t, `SELECT slab_links::text FROM notes WHERE id = $1`, seedNote)
	if links != `[{"path": "/slab/cidr", "label": "CIDR Calculator"}]` {
		t.Errorf("slab links = %s", links)
	}

	// AC-S4: both tables, complete, with the corrected /78 value.
	content := queryString(t, `SELECT content FROM notes WHERE id = $1`, seedNote)
	ipv4Rows := regexp.MustCompile(`(?m)^\| \d+ \| /\d+ \| \d+\.\d+\.\d+\.\d+ \| [\d,]+ \|$`).FindAllString(content, -1)
	ipv6Rows := regexp.MustCompile(`(?m)^\| \d+ \| /\d+ \| [\d,]+ \|$`).FindAllString(content, -1)
	if len(ipv4Rows) != 33 || len(ipv6Rows) != 129 {
		t.Errorf("table rows = %d IPv4 / %d IPv6, want 33 / 129", len(ipv4Rows), len(ipv6Rows))
	}
	if !strings.Contains(content, "| 50 | /78 | 1,125,899,906,842,624 |") {
		t.Error("the /78 row must read 1,125,899,906,842,624")
	}
	if !strings.Contains(content, "lists `/78` as 1,125,899,906,842,620") {
		t.Error("the note must carry the /78 correction")
	}
	if !strings.Contains(content, "## Source") || !strings.Contains(content, "A Comprehensive Guide to IPv4 and IPv6 CIDR") {
		t.Error("the note must credit its source article")
	}
	if !strings.Contains(content, "](/slab/cidr)") {
		t.Error("the note must link to the calculator")
	}
}

func TestSeedReusesAnExistingTIAndKeepsItUntouched(t *testing.T) {
	skipShort(t)
	freshUpTo(t, seedVersion-1)
	exec(t, `INSERT INTO categories (id, name, color) VALUES ('01a120d9-cd11-719e-b53b-6efe59ff8f0e', 'ti', '#000000')`)

	migrateTo(t, seedVersion)

	if n := queryInt(t, `SELECT count(*) FROM categories WHERE parent_id IS NULL AND lower(name) = 'ti' AND deleted_at IS NULL`); n != 1 {
		t.Fatalf("alive TI roots = %d, want 1 (reused, not duplicated)", n)
	}
	if got := queryString(t, `SELECT name || ' ' || color FROM categories WHERE id = '01a120d9-cd11-719e-b53b-6efe59ff8f0e'`); got != "ti #000000" {
		t.Errorf("existing TI changed to %q", got)
	}
	if got := queryString(t, `SELECT parent_id::text FROM categories WHERE name = 'Networks' AND deleted_at IS NULL`); got != "01a120d9-cd11-719e-b53b-6efe59ff8f0e" {
		t.Errorf("Networks parent = %s, want the existing TI", got)
	}
}

func TestSeedReusesExistingNetworksAndSubject(t *testing.T) {
	skipShort(t)
	freshUpTo(t, seedVersion-1)
	exec(t, `INSERT INTO categories (id, name, color) VALUES ('11111111-0000-7000-8000-000000000001', 'TI', '#000000')`)
	exec(t, `INSERT INTO categories (id, parent_id, name, color) VALUES ('11111111-0000-7000-8000-000000000002', '11111111-0000-7000-8000-000000000001', 'networks', '#111111')`)
	exec(t, `INSERT INTO subjects (id, category_id, name) VALUES ('11111111-0000-7000-8000-000000000003', '11111111-0000-7000-8000-000000000002', 'cidr')`)

	migrateTo(t, seedVersion)

	if n := queryInt(t, `SELECT count(*) FROM categories WHERE deleted_at IS NULL`); n != 2 {
		t.Errorf("alive categories = %d, want 2 (no duplicates)", n)
	}
	if n := queryInt(t, `SELECT count(*) FROM subjects WHERE deleted_at IS NULL`); n != 1 {
		t.Errorf("alive subjects = %d, want 1", n)
	}
	if got := queryString(t, `SELECT subject_id::text FROM notes WHERE title = $1 AND deleted_at IS NULL`, seedTitle); got != "11111111-0000-7000-8000-000000000003" {
		t.Errorf("note subject = %s, want the existing cidr subject", got)
	}
	if got := queryString(t, `SELECT color FROM categories WHERE id = '11111111-0000-7000-8000-000000000002'`); got != "#111111" {
		t.Errorf("existing networks recolored to %s", got)
	}
}

func TestSeedDownIsLogicalAndUpRevives(t *testing.T) {
	skipShort(t)
	m := freshUpTo(t, seedVersion)

	// AC-S3: down logically deletes exactly the seeded rows…
	if err := m.Migrate(seedVersion - 1); err != nil {
		t.Fatalf("migrate down one: %v", err)
	}
	if n := queryInt(t, `SELECT count(*) FROM categories WHERE id IN ($1, $2) AND deleted_at IS NOT NULL`, seedTI, seedNetwork); n != 2 {
		t.Errorf("logically deleted seeded categories = %d, want 2", n)
	}
	if n := queryInt(t, `SELECT count(*) FROM subjects WHERE id = $1 AND deleted_at IS NOT NULL`, seedSubject); n != 1 {
		t.Error("seeded subject must be logically deleted, not removed")
	}
	if n := queryInt(t, `SELECT count(*) FROM notes WHERE id = $1 AND deleted_at IS NOT NULL`, seedNote); n != 1 {
		t.Error("seeded note must be logically deleted, not removed")
	}

	// …and up again revives them instead of colliding on the fixed ids.
	if err := m.Migrate(seedVersion); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if n := queryInt(t, `SELECT count(*) FROM notes WHERE id = $1 AND deleted_at IS NULL`, seedNote); n != 1 {
		t.Error("seeded note not revived")
	}
	if n := queryInt(t, `SELECT count(*) FROM note_tags WHERE note_id = $1 AND deleted_at IS NULL`, seedNote); n != 5 {
		t.Errorf("revived tag links = %d, want 5", n)
	}
}

func TestSeedDownKeepsAPreexistingTI(t *testing.T) {
	skipShort(t)
	freshUpTo(t, seedVersion-1)
	exec(t, `INSERT INTO categories (id, name, color) VALUES ('22222222-0000-7000-8000-000000000001', 'TI', '#000000')`)
	m := migrateTo(t, seedVersion)

	if err := m.Migrate(seedVersion - 1); err != nil {
		t.Fatalf("migrate down one: %v", err)
	}
	if n := queryInt(t, `SELECT count(*) FROM categories WHERE id = '22222222-0000-7000-8000-000000000001' AND deleted_at IS NULL`); n != 1 {
		t.Error("the owner's TI must survive the seed's down migration")
	}
}
