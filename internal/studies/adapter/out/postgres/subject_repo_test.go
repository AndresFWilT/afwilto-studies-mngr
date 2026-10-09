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

type subjRepoEnv struct {
	cats *catRepoEnv
	repo *SubjectRepository
	ids  *testsupport.SeqIDGen
	now  time.Time
}

func newSubjRepoEnv(t *testing.T) *subjRepoEnv {
	t.Helper()
	cats := newCatRepoEnv(t) // truncates categories CASCADE (subjects included)
	return &subjRepoEnv{cats: cats, repo: NewSubjectRepository(pool), ids: &testsupport.SeqIDGen{N: 1000}, now: testsupport.FixedNow}
}

func (e *subjRepoEnv) subject(t *testing.T, categoryID, name string) domain.Subject {
	t.Helper()
	n, err := domain.NewSubjectName(name)
	if err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	return domain.NewSubject(e.ids.NewID(), categoryID, n, nil, e.now)
}

func (e *subjRepoEnv) add(t *testing.T, categoryID, name string) domain.Subject {
	t.Helper()
	s := e.subject(t, categoryID, name)
	if err := e.repo.Create(context.Background(), s); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return s
}

func TestSubjectRepoCreateAndGetViewWithCategoryPath(t *testing.T) {
	e := newSubjRepoEnv(t)
	c := e.cats.chainOf(t, "TI", "Cloud", "AWS")
	desc := "line1\nline2"
	s := e.subject(t, c[1].ID, "Certified Cloud Practitioner")
	s.Description = &desc
	if err := e.repo.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	v, err := e.repo.GetView(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Category.ID != c[1].ID || v.Category.Path != "TI / Cloud" || v.Category.Name != "Cloud" || v.Category.Color != "#22D3EE" {
		t.Errorf("category ref = %+v", v.Category)
	}
	if v.Description == nil || *v.Description != desc || v.NoteCount != 0 || v.Name.String() != "Certified Cloud Practitioner" {
		t.Errorf("view = %+v", v)
	}
}

func TestSubjectRepoNameUniquePerCategoryAmongAlive(t *testing.T) {
	e := newSubjRepoEnv(t)
	a := e.cats.add(t, "A", nil)
	b := e.cats.add(t, "B", nil)
	s := e.add(t, a.ID, "Networking")
	e.add(t, b.ID, "Networking")

	err := e.repo.Create(context.Background(), e.subject(t, a.ID, "NETWORKING"))
	wantConflictErr(t, err, domain.RuleDuplicateName)

	s.Delete(e.now.Add(time.Hour))
	if err := e.repo.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	e.add(t, a.ID, "Networking") // freed
}

func TestSubjectRepoListOrderAndFilter(t *testing.T) {
	e := newSubjRepoEnv(t)
	c := e.cats.chainOf(t, "TI", "Cloud")
	e.add(t, c[0].ID, "zeta")
	e.add(t, c[0].ID, "Alpha")
	e.add(t, c[1].ID, "Deep")

	names := func(filter *string) []string {
		list, err := e.repo.List(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, v := range list {
			out = append(out, v.Name.String())
		}
		return out
	}
	if got := names(nil); len(got) != 3 || got[0] != "Alpha" || got[1] != "Deep" || got[2] != "zeta" {
		t.Errorf("all = %v", got)
	}
	if got := names(&c[0].ID); len(got) != 2 || got[0] != "Alpha" || got[1] != "zeta" {
		t.Errorf("direct members of TI = %v", got)
	}
}

func TestSubjectRepoMoveAndLogicalDelete(t *testing.T) {
	e := newSubjRepoEnv(t)
	a := e.cats.add(t, "A", nil)
	b := e.cats.add(t, "B", nil)
	s := e.add(t, a.ID, "Net")

	s.CategoryID = b.ID
	if err := e.repo.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.repo.GetView(context.Background(), s.ID); v.Category.ID != b.ID {
		t.Errorf("moved to %s, want %s", v.Category.ID, b.ID)
	}

	s.Delete(e.now.Add(time.Hour))
	if err := e.repo.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.Get(context.Background(), s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
	if err := e.repo.Update(context.Background(), s); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update deleted = %v, want ErrNotFound", err)
	}
	var stamped bool
	if err := pool.QueryRow(context.Background(), `SELECT deleted_at IS NOT NULL FROM subjects WHERE id=$1`, s.ID).Scan(&stamped); err != nil || !stamped {
		t.Errorf("row must remain with deleted_at (stamped=%v err=%v)", stamped, err)
	}
	if n, _ := e.repo.CountAliveInCategory(context.Background(), b.ID); n != 0 {
		t.Errorf("CountAliveInCategory = %d, want 0", n)
	}
}

func TestSubjectCountInCategoryView(t *testing.T) { // S04-10
	e := newSubjRepoEnv(t)
	c := e.cats.chainOf(t, "TI", "Cloud")
	e.add(t, c[0].ID, "One")
	two := e.add(t, c[0].ID, "Two")
	e.add(t, c[1].ID, "Child")
	two.Delete(e.now.Add(time.Hour))
	if err := e.repo.Update(context.Background(), two); err != nil {
		t.Fatal(err)
	}

	list, err := e.cats.repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list[0].SubjectCount != 1 || list[1].SubjectCount != 1 {
		t.Errorf("subjectCount = %d, %d; want 1, 1", list[0].SubjectCount, list[1].SubjectCount)
	}
}

func TestSubjectsRejectMissingOrDeletedCategory(t *testing.T) { // S04-12
	e := newSubjRepoEnv(t)
	err := e.repo.Create(context.Background(), e.subject(t, "0abcdef0-0000-7000-8000-999999999999", "N"))
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Violations[0].Field != "categoryId" || v.Violations[0].Rule != "not_found" {
		t.Fatalf("missing category: err = %v", err)
	}

	c := e.cats.add(t, "A", nil)
	c.Delete(e.now)
	if err := e.cats.repo.Update(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	err = e.repo.Create(context.Background(), e.subject(t, c.ID, "N"))
	if !errors.As(err, &v) || v.Violations[0].Rule != "not_found" {
		t.Fatalf("deleted category: err = %v", err)
	}
}

func TestCategoryDeleteRejectedByTriggerWhileSubjectsAlive(t *testing.T) { // S04-12
	e := newSubjRepoEnv(t)
	c := e.cats.add(t, "A", nil)
	e.add(t, c.ID, "Net")

	c.Delete(e.now)
	wantConflictErr(t, e.cats.repo.Update(context.Background(), c), domain.RuleInUse)
}

func TestSubjectsNoHardDeleteTrigger(t *testing.T) { // S04-14
	e := newSubjRepoEnv(t)
	c := e.cats.add(t, "A", nil)
	e.add(t, c.ID, "Net")
	_, err := pool.Exec(context.Background(), `DELETE FROM subjects`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
		t.Fatalf("err = %v, want SQLSTATE 23001", err)
	}
}

func TestSubjectMoveToDeletedCategoryRejectedByTrigger(t *testing.T) { // S04-12, UPDATE branch
	e := newSubjRepoEnv(t)
	from := e.cats.add(t, "From", nil)
	gone := e.cats.add(t, "Gone", nil)
	s := e.add(t, from.ID, "Net")

	gone.Delete(e.now)
	if err := e.cats.repo.Update(context.Background(), gone); err != nil {
		t.Fatal(err)
	}
	s.CategoryID = gone.ID
	err := e.repo.Update(context.Background(), s)
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Violations[0].Field != "categoryId" || v.Violations[0].Rule != "not_found" {
		t.Fatalf("err = %v, want categoryId/not_found", err)
	}
}
