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

type catRepoEnv struct {
	repo *CategoryRepository
	ids  *testsupport.SeqIDGen
	now  time.Time
}

func newCatRepoEnv(t *testing.T) *catRepoEnv {
	t.Helper()
	skipShort(t)
	exec(t, `TRUNCATE categories CASCADE`)
	return &catRepoEnv{repo: NewCategoryRepository(pool), ids: &testsupport.SeqIDGen{}, now: testsupport.FixedNow}
}

func (e *catRepoEnv) add(t *testing.T, name string, parent *domain.Category) domain.Category {
	t.Helper()
	n, err := domain.NewCategoryName(name)
	if err != nil {
		t.Fatal(err)
	}
	var parentID *string
	if parent != nil {
		parentID = &parent.ID
	}
	e.now = e.now.Add(time.Minute)
	cat := domain.NewCategory(e.ids.NewID(), n, domain.RehydrateHexColor("#22D3EE"), parentID, e.now)
	if err := e.repo.Create(context.Background(), cat); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return cat
}

func (e *catRepoEnv) chainOf(t *testing.T, names ...string) []domain.Category {
	t.Helper()
	var out []domain.Category
	var parent *domain.Category
	for _, n := range names {
		c := e.add(t, n, parent)
		out = append(out, c)
		parent = &out[len(out)-1]
	}
	return out
}

func wantConflictErr(t *testing.T, err error, rule string) {
	t.Helper()
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != rule {
		t.Fatalf("err = %v, want ConflictError(%s)", err, rule)
	}
}

func TestCategoryRepoCreateAndGetView(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "TI", "Cloud", "AWS")

	v, err := e.repo.GetView(context.Background(), c[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Path != "TI / Cloud / AWS" || v.Depth != 3 || v.SubjectCount != 0 {
		t.Errorf("view = path %q depth %d subjects %d", v.Path, v.Depth, v.SubjectCount)
	}
	if v.ParentID == nil || *v.ParentID != c[1].ID || v.Name.String() != "AWS" || v.Color.String() != "#22D3EE" {
		t.Errorf("view = %+v", v)
	}
	if !v.CreatedAt.Equal(c[2].CreatedAt) || !v.UpdatedAt.Equal(c[2].UpdatedAt) {
		t.Errorf("timestamps = %v/%v, want %v", v.CreatedAt, v.UpdatedAt, c[2].CreatedAt)
	}
}

func TestCategoryRepoListOrderAndVisibility(t *testing.T) {
	e := newCatRepoEnv(t)
	ti := e.add(t, "TI", nil)
	e.add(t, "zeta", &ti)
	e.add(t, "Cloud", &ti)
	e.add(t, "art", nil)
	e.add(t, "TI-Extra", nil)
	gone := e.add(t, "Gone", nil)
	gone.Delete(e.now.Add(time.Minute))
	if err := e.repo.Update(context.Background(), gone); err != nil {
		t.Fatal(err)
	}

	list, err := e.repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"art", "TI", "TI / Cloud", "TI / zeta", "TI-Extra"}
	if len(list) != len(want) {
		t.Fatalf("got %d categories, want %v", len(list), want)
	}
	for i, v := range list {
		if v.Path != want[i] {
			t.Errorf("list[%d] = %q, want %q", i, v.Path, want[i])
		}
	}
}

func TestCategoryRepoSiblingUniqueness(t *testing.T) {
	e := newCatRepoEnv(t)
	ti := e.add(t, "TI", nil)
	other := e.add(t, "Other", nil)
	cloud := e.add(t, "Cloud", &ti)
	e.add(t, "Cloud", &other)

	dup := domain.NewCategory(e.ids.NewID(), domain.RehydrateCategoryName("cloud"), domain.RehydrateHexColor("#000000"), &ti.ID, e.now)
	wantConflictErr(t, e.repo.Create(context.Background(), dup), domain.RuleDuplicateName)

	rootDup := domain.NewCategory(e.ids.NewID(), domain.RehydrateCategoryName("ti"), domain.RehydrateHexColor("#000000"), nil, e.now)
	wantConflictErr(t, e.repo.Create(context.Background(), rootDup), domain.RuleDuplicateName)

	cloud.Delete(e.now)
	if err := e.repo.Update(context.Background(), cloud); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(context.Background(), dup); err != nil {
		t.Errorf("name of a deleted sibling must be reusable: %v", err)
	}
}

func TestCategoryRepoUpdateRenameLeavesChildRowsUntouched(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "TI", "Cloud", "AWS")

	cloud := c[1]
	cloud.Name = domain.RehydrateCategoryName("Cloud Computing")
	cloud.UpdatedAt = e.now.Add(time.Hour)
	if err := e.repo.Update(context.Background(), cloud); err != nil {
		t.Fatal(err)
	}

	aws, err := e.repo.GetView(context.Background(), c[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if aws.Path != "TI / Cloud Computing / AWS" {
		t.Errorf("AWS path = %q", aws.Path)
	}
	if !aws.UpdatedAt.Equal(c[2].UpdatedAt) {
		t.Errorf("AWS updated_at changed to %v; a rename must not write descendants", aws.UpdatedAt)
	}
}

func TestCategoryRepoUpdateDuplicateAndNotFound(t *testing.T) {
	e := newCatRepoEnv(t)
	ti := e.add(t, "TI", nil)
	data := e.add(t, "Data", nil)
	data.Name = domain.RehydrateCategoryName("ti")
	wantConflictErr(t, e.repo.Update(context.Background(), data), domain.RuleDuplicateName)

	ti.Delete(e.now)
	if err := e.repo.Update(context.Background(), ti); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Update(context.Background(), ti); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update of a deleted row: err = %v, want ErrNotFound", err)
	}
}

func TestCategoryRepoLogicalDeleteKeepsRow(t *testing.T) {
	e := newCatRepoEnv(t)
	ti := e.add(t, "TI", nil)
	ti.Delete(e.now.Add(time.Hour))
	if err := e.repo.Update(context.Background(), ti); err != nil {
		t.Fatal(err)
	}

	if _, err := e.repo.Get(context.Background(), ti.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrNotFound", err)
	}
	if _, err := e.repo.GetView(context.Background(), ti.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetView after delete: err = %v, want ErrNotFound", err)
	}
	var deletedAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT deleted_at FROM categories WHERE id = $1`, ti.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("row must still exist: %v", err)
	}
	if deletedAt == nil || !deletedAt.Equal(*ti.DeletedAt) {
		t.Errorf("deleted_at = %v, want %v", deletedAt, ti.DeletedAt)
	}
}

func TestCategoryRepoTreeQueries(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "TI", "Cloud", "AWS")
	e.add(t, "Data", &c[0])
	gone := e.add(t, "Gone", &c[2]) // deleted: must not count toward height or children
	gone.Delete(e.now)
	if err := e.repo.Update(context.Background(), gone); err != nil {
		t.Fatal(err)
	}
	bg := context.Background()

	ids, err := e.repo.AncestorIDs(bg, c[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != c[0].ID || ids[1] != c[1].ID || ids[2] != c[2].ID {
		t.Errorf("AncestorIDs = %v, want root → AWS", ids)
	}
	if _, err := e.repo.AncestorIDs(bg, gone.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("AncestorIDs(deleted) err = %v, want ErrNotFound", err)
	}

	for _, tc := range []struct {
		id   string
		want int
	}{{c[0].ID, 3}, {c[1].ID, 2}, {c[2].ID, 1}} {
		if h, err := e.repo.SubtreeHeight(bg, tc.id); err != nil || h != tc.want {
			t.Errorf("SubtreeHeight = %d, %v; want %d", h, err, tc.want)
		}
	}

	if n, err := e.repo.CountAliveChildren(bg, c[0].ID); err != nil || n != 2 {
		t.Errorf("CountAliveChildren(TI) = %d, %v; want 2", n, err)
	}
	if n, err := e.repo.CountAliveChildren(bg, c[2].ID); err != nil || n != 0 {
		t.Errorf("CountAliveChildren(AWS) = %d, %v; want 0 (child deleted)", n, err)
	}
}

func TestCategoriesTableForbidsHardDelete(t *testing.T) {
	e := newCatRepoEnv(t)
	e.add(t, "TI", nil)

	_, err := pool.Exec(context.Background(), `DELETE FROM categories`)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
		t.Fatalf("DELETE FROM categories: err = %v, want SQLSTATE 23001", err)
	}
}

// ── Database-level tree guard (S02-8) ───────────────────────────────────────

func wantViolationErr(t *testing.T, err error, field, rule string) {
	t.Helper()
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want ValidationError(%s/%s)", err, field, rule)
	}
	for _, viol := range v.Violations {
		if viol.Field == field && viol.Rule == rule {
			return
		}
	}
	t.Errorf("violations = %+v, want %s/%s", v.Violations, field, rule)
}

func TestTreeGuardRejectsCycleEvenWithoutUseCase(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "TI", "Cloud", "AWS")

	ti := c[0]
	ti.ParentID = &c[2].ID
	wantViolationErr(t, e.repo.Update(context.Background(), ti), "parentId", "cycle")

	ti.ParentID = &c[0].ID
	wantViolationErr(t, e.repo.Update(context.Background(), ti), "parentId", "cycle")

	if _, err := pool.Exec(context.Background(), `UPDATE categories SET parent_id = $1 WHERE id = $2`, c[1].ID, c[0].ID); err == nil {
		t.Error("raw SQL cycle was accepted")
	}
}

func TestTreeGuardRejectsDepthOverFive(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "L1", "L2", "L3", "L4", "L5")
	n := domain.NewCategory(e.ids.NewID(), domain.RehydrateCategoryName("L6"), domain.RehydrateHexColor("#000000"), &c[4].ID, e.now)
	wantViolationErr(t, e.repo.Create(context.Background(), n), "parentId", "max_depth")

	sub := e.chainOf(t, "S1", "S2")
	s1 := sub[0]
	s1.ParentID = &c[3].ID // depth 4 + height 2
	wantViolationErr(t, e.repo.Update(context.Background(), s1), "parentId", "max_depth")
}

func TestTreeGuardRejectsDeletedParent(t *testing.T) {
	e := newCatRepoEnv(t)
	gone := e.add(t, "Gone", nil)
	gone.Delete(e.now)
	if err := e.repo.Update(context.Background(), gone); err != nil {
		t.Fatal(err)
	}
	n := domain.NewCategory(e.ids.NewID(), domain.RehydrateCategoryName("X"), domain.RehydrateHexColor("#000000"), &gone.ID, e.now)
	wantViolationErr(t, e.repo.Create(context.Background(), n), "parentId", "not_found")
}

func TestTreeGuardBlocksDeletingAParentWithAliveChildren(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "TI", "Cloud")
	ti := c[0]
	ti.Delete(e.now)
	wantConflictErr(t, e.repo.Update(context.Background(), ti), domain.RuleInUse)
}

func TestTreeGuardChecksManualRestore(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "TI", "Cloud")
	cloud, ti := c[1], c[0]
	cloud.Delete(e.now)
	if err := e.repo.Update(context.Background(), cloud); err != nil {
		t.Fatal(err)
	}
	ti.Delete(e.now)
	if err := e.repo.Update(context.Background(), ti); err != nil {
		t.Fatal(err)
	}
	// restoring a child whose parent is still deleted must fail
	if _, err := pool.Exec(context.Background(), `UPDATE categories SET deleted_at = NULL WHERE id = $1`, cloud.ID); err == nil {
		t.Error("restored a child under a deleted parent")
	}
}

func TestRecursiveQueriesTerminateOnACorruptedTree(t *testing.T) {
	e := newCatRepoEnv(t)
	c := e.chainOf(t, "A", "B")
	// Corrupt the tree behind the guard's back. SET LOCAL scopes the disabled
	// triggers to this one transaction, so no pooled connection keeps them off.
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE categories SET parent_id = $1 WHERE id = $2`, c[1].ID, c[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec(t, `TRUNCATE categories CASCADE`) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := e.repo.SubtreeHeight(ctx, c[0].ID); ctx.Err() != nil {
		t.Errorf("SubtreeHeight hung on a cycle (err %v)", err)
	}
	if _, err := e.repo.AncestorIDs(ctx, c[0].ID); ctx.Err() != nil {
		t.Errorf("AncestorIDs hung on a cycle (err %v)", err)
	}
}

// Two opposite moves racing (A under B, B under A) must not both commit: the
// trigger's advisory lock makes the second wait and then see the first.
func TestTreeGuardSerializesConcurrentMoves(t *testing.T) {
	e := newCatRepoEnv(t)
	a := e.add(t, "A", nil)
	b := e.add(t, "B", nil)
	bg := context.Background()

	tx1, err := pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx1.Rollback(bg) }()
	if _, err := tx1.Exec(bg, `UPDATE categories SET parent_id = $1 WHERE id = $2`, b.ID, a.ID); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		moved := b
		moved.ParentID = &a.ID
		done <- e.repo.Update(bg, moved)
	}()

	select {
	case err := <-done:
		t.Fatalf("second move finished while the first was uncommitted (err %v); it must wait for the tree lock", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx1.Commit(bg); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		wantViolationErr(t, err, "parentId", "cycle")
	case <-time.After(5 * time.Second):
		t.Fatal("second move never finished")
	}
}
