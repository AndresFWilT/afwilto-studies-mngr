package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

type kindRepoEnv struct {
	repo *NoteKindRepository
	ids  *testsupport.SeqIDGen
	now  time.Time
}

// newKindRepoEnv starts from an empty table; the seed is checked separately.
func newKindRepoEnv(t *testing.T) *kindRepoEnv {
	t.Helper()
	skipShort(t)
	exec(t, `TRUNCATE note_kinds CASCADE`)
	return &kindRepoEnv{repo: NewNoteKindRepository(pool), ids: &testsupport.SeqIDGen{}, now: testsupport.FixedNow}
}

func (e *kindRepoEnv) add(t *testing.T, name string) domain.NoteKind {
	t.Helper()
	n, err := domain.NewNoteKindName(name)
	if err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	k := domain.NewNoteKind(e.ids.NewID(), n, e.now)
	if err := e.repo.Create(context.Background(), k); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return k
}

// Other tests TRUNCATE note_kinds, so the seed is verified on a freshly
// re-applied migration set.
func TestNoteKindsSeedAfterMigration(t *testing.T) {
	skipShort(t)
	m, err := newMigrate()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	list, err := NewNoteKindRepository(pool).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ id, name string }{
		{"00000000-0000-7000-8000-000000000002", "Cheat sheet"},
		{"00000000-0000-7000-8000-000000000003", "Notes"},
		{"00000000-0000-7000-8000-000000000001", "Summary"},
	}
	if len(list) != len(want) {
		t.Fatalf("seed has %d kinds, want %d", len(list), len(want))
	}
	for i, w := range want {
		if list[i].ID != w.id || list[i].Name.String() != w.name {
			t.Errorf("seed[%d] = %s/%s, want %s/%s", i, list[i].ID, list[i].Name.String(), w.id, w.name)
		}
	}
}

func TestNoteKindRepoCreateGetAndOrder(t *testing.T) {
	e := newKindRepoEnv(t)
	e.add(t, "Summary")
	e.add(t, "cheat sheet")
	e.add(t, "Notes")

	list, err := e.repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range list {
		got = append(got, v.Name.String())
		if v.NoteCount != 0 {
			t.Errorf("noteCount = %d, want 0", v.NoteCount)
		}
	}
	want := []string{"cheat sheet", "Notes", "Summary"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestNoteKindRepoDuplicateAmongAliveOnly(t *testing.T) {
	e := newKindRepoEnv(t)
	k := e.add(t, "Notes")

	n, _ := domain.NewNoteKindName("NOTES")
	err := e.repo.Create(context.Background(), domain.NewNoteKind(e.ids.NewID(), n, e.now))
	wantConflictErr(t, err, domain.RuleDuplicateName)

	k.Delete(e.now.Add(time.Hour))
	if err := e.repo.Update(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	e.add(t, "Notes") // freed
}

func TestNoteKindRepoLogicalDeleteKeepsRow(t *testing.T) {
	e := newKindRepoEnv(t)
	k := e.add(t, "Notes")
	k.Delete(e.now.Add(time.Hour))
	if err := e.repo.Update(context.Background(), k); err != nil {
		t.Fatal(err)
	}

	if _, err := e.repo.Get(context.Background(), k.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	if _, err := e.repo.GetView(context.Background(), k.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetView = %v, want ErrNotFound", err)
	}
	if err := e.repo.Update(context.Background(), k); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update deleted = %v, want ErrNotFound", err)
	}
	var stamped bool
	if err := pool.QueryRow(context.Background(), `SELECT deleted_at IS NOT NULL FROM note_kinds WHERE id = $1`, k.ID).Scan(&stamped); err != nil || !stamped {
		t.Fatalf("row must remain with deleted_at (stamped=%v, err=%v)", stamped, err)
	}
}

func TestNoteKindsNoHardDeleteTrigger(t *testing.T) {
	e := newKindRepoEnv(t)
	e.add(t, "Notes")
	_, err := pool.Exec(context.Background(), `DELETE FROM note_kinds`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
		t.Fatalf("err = %v, want SQLSTATE 23001", err)
	}
}
