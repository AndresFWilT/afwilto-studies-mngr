package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

type catEnv struct {
	repo   *testsupport.FakeCategoryRepo
	subs   *testsupport.FakeSubjectRepo
	create in.CreateCategory
	get    in.GetCategory
	list   in.ListCategories
	update in.UpdateCategory
	delete in.DeleteCategory
}

func newCatEnv() catEnv {
	repo := testsupport.NewFakeCategoryRepo()
	subs := testsupport.NewFakeSubjectRepo(repo)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	return catEnv{
		repo:   repo,
		subs:   subs,
		create: NewCreateCategoryService(repo, clock, ids),
		get:    NewGetCategoryService(repo),
		list:   NewListCategoriesService(repo),
		update: NewUpdateCategoryService(repo, clock),
		delete: NewDeleteCategoryService(repo, subs, clock),
	}
}

var ctx = context.Background()

func ptr(s string) *string { return &s }

func set(s string) in.Patch[string] { return in.Patch[string]{Set: true, Value: &s} }
func setNull() in.Patch[string]     { return in.Patch[string]{Set: true} }

func (e catEnv) mustCreate(t *testing.T, name string, parent *domain.CategoryView) domain.CategoryView {
	t.Helper()
	cmd := in.CreateCategoryCommand{Name: name, Color: "#22d3ee"}
	if parent != nil {
		cmd.ParentID = &parent.ID
	}
	v, err := e.create.Execute(ctx, cmd)
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return v
}

// chainOf creates root → … → leaf named n1..nk and returns them in order.
func (e catEnv) chainOf(t *testing.T, names ...string) []domain.CategoryView {
	t.Helper()
	var out []domain.CategoryView
	var parent *domain.CategoryView
	for _, n := range names {
		v := e.mustCreate(t, n, parent)
		out = append(out, v)
		parent = &out[len(out)-1]
	}
	return out
}

func wantViolation(t *testing.T, err error, field, rule string) {
	t.Helper()
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want *ValidationError(%s/%s)", err, field, rule)
	}
	for _, viol := range v.Violations {
		if viol.Field == field && viol.Rule == rule {
			return
		}
	}
	t.Errorf("violations = %+v, want field %q rule %q", v.Violations, field, rule)
}

func wantConflict(t *testing.T, err error, rule string) {
	t.Helper()
	var c *domain.ConflictError
	if !errors.As(err, &c) {
		t.Fatalf("err = %v, want *ConflictError(%s)", err, rule)
	}
	if c.Rule != rule {
		t.Errorf("conflict rule = %q, want %q", c.Rule, rule)
	}
}

func wantNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// ── Create (AC-B1, B2, B3, B8) ──────────────────────────────────────────────

func TestCreateRootCategory(t *testing.T) {
	e := newCatEnv()
	v, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: " TI ", Color: "#ff9900"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Name.String() != "TI" || v.Color.String() != "#FF9900" || v.ParentID != nil || v.Path != "TI" || v.Depth != 1 {
		t.Errorf("view = %+v", v)
	}
	if v.CreatedAt.IsZero() || !v.CreatedAt.Equal(v.UpdatedAt) {
		t.Errorf("timestamps = %v / %v", v.CreatedAt, v.UpdatedAt)
	}
}

func TestCreateNestedCategoryComputesPathAndDepth(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud", "AWS")
	if c[2].Path != "TI / Cloud / AWS" || c[2].Depth != 3 {
		t.Errorf("AWS = path %q depth %d, want 'TI / Cloud / AWS' depth 3", c[2].Path, c[2].Depth)
	}
}

func TestCreateSiblingUniqueness(t *testing.T) {
	e := newCatEnv()
	ti := e.mustCreate(t, "TI", nil)
	other := e.mustCreate(t, "Other", nil)
	e.mustCreate(t, "Cloud", &ti)

	if _, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "Cloud", Color: "#000000", ParentID: &other.ID}); err != nil {
		t.Errorf("same name under another parent: err = %v, want nil", err)
	}
	_, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "cloud", Color: "#000000", ParentID: &ti.ID})
	wantConflict(t, err, domain.RuleDuplicateName)
	_, err = e.create.Execute(ctx, in.CreateCategoryCommand{Name: "ti", Color: "#000000"})
	wantConflict(t, err, domain.RuleDuplicateName)
}

func TestCreateRejectsInvalidFields(t *testing.T) {
	e := newCatEnv()
	_, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "", Color: "#000000"})
	wantViolation(t, err, "name", "required")
	_, err = e.create.Execute(ctx, in.CreateCategoryCommand{Name: "X", Color: "blue"})
	wantViolation(t, err, "color", "format")
}

func TestCreateUnderMissingOrDeletedParent(t *testing.T) {
	e := newCatEnv()
	_, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "X", Color: "#000000", ParentID: ptr("00000000-0000-7000-8000-999999999999")})
	wantViolation(t, err, "parentId", "not_found")

	gone := e.mustCreate(t, "Gone", nil)
	if err := e.delete.Execute(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.create.Execute(ctx, in.CreateCategoryCommand{Name: "X", Color: "#000000", ParentID: &gone.ID})
	wantViolation(t, err, "parentId", "not_found")
}

func TestCreateRespectsMaxDepth(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "L1", "L2", "L3", "L4", "L5")
	if c[4].Depth != 5 {
		t.Fatalf("depth = %d, want 5", c[4].Depth)
	}
	_, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "L6", Color: "#000000", ParentID: &c[4].ID})
	wantViolation(t, err, "parentId", "max_depth")
}

// ── Get / List (AC-B7) ──────────────────────────────────────────────────────

func TestListOrdersBySegmentsCaseInsensitive(t *testing.T) {
	e := newCatEnv()
	ti := e.mustCreate(t, "TI", nil)
	e.mustCreate(t, "zeta", &ti)
	e.mustCreate(t, "Cloud", &ti)
	e.mustCreate(t, "art", nil)
	e.mustCreate(t, "TI-Extra", nil)

	list, err := e.list.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range list {
		got = append(got, v.Path)
	}
	want := []string{"art", "TI", "TI / Cloud", "TI / zeta", "TI-Extra"}
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths = %v, want %v", got, want)
		}
	}
}

func TestGetUnknownCategory(t *testing.T) {
	e := newCatEnv()
	_, err := e.get.Execute(ctx, "00000000-0000-7000-8000-999999999999")
	wantNotFound(t, err)
}

// ── Update (AC-B3, B4, B5, S02-2) ───────────────────────────────────────────

func TestRenameParentChangesDescendantPath(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud", "AWS")
	before := e.repo.Rows[c[2].ID]

	if _, err := e.update.Execute(ctx, c[1].ID, in.UpdateCategoryCommand{Name: set("Cloud Computing")}); err != nil {
		t.Fatal(err)
	}

	aws, err := e.get.Execute(ctx, c[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if aws.Path != "TI / Cloud Computing / AWS" {
		t.Errorf("AWS path = %q", aws.Path)
	}
	if after := e.repo.Rows[c[2].ID]; !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Error("AWS row was written; a rename must touch only the renamed row")
	}
}

func TestUpdateRecolorKeepsNameAndParent(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud")
	v, err := e.update.Execute(ctx, c[1].ID, in.UpdateCategoryCommand{Color: set("#34d399")})
	if err != nil {
		t.Fatal(err)
	}
	if v.Color.String() != "#34D399" || v.Name.String() != "Cloud" || v.ParentID == nil || *v.ParentID != c[0].ID {
		t.Errorf("view = %+v", v)
	}
	if !v.UpdatedAt.After(v.CreatedAt) {
		t.Errorf("UpdatedAt %v not after CreatedAt %v", v.UpdatedAt, v.CreatedAt)
	}
}

func TestUpdateNullParentMovesToRoot(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud", "AWS")
	v, err := e.update.Execute(ctx, c[1].ID, in.UpdateCategoryCommand{ParentID: setNull()})
	if err != nil {
		t.Fatal(err)
	}
	if v.ParentID != nil || v.Depth != 1 || v.Path != "Cloud" {
		t.Errorf("moved = %+v", v)
	}
	aws, _ := e.get.Execute(ctx, c[2].ID)
	if aws.Path != "Cloud / AWS" {
		t.Errorf("AWS path = %q, want subtree moved along", aws.Path)
	}
}

func TestUpdateMoveUnderOtherParent(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud")
	other := e.mustCreate(t, "Other", nil)
	v, err := e.update.Execute(ctx, c[1].ID, in.UpdateCategoryCommand{ParentID: set(other.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if v.Path != "Other / Cloud" {
		t.Errorf("path = %q", v.Path)
	}
}

func TestUpdateMoveRejectsCycles(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud", "AWS")
	_, err := e.update.Execute(ctx, c[0].ID, in.UpdateCategoryCommand{ParentID: set(c[2].ID)})
	wantViolation(t, err, "parentId", "cycle")
	_, err = e.update.Execute(ctx, c[0].ID, in.UpdateCategoryCommand{ParentID: set(c[0].ID)})
	wantViolation(t, err, "parentId", "cycle")
}

func TestUpdateMoveRespectsMaxDepth(t *testing.T) {
	e := newCatEnv()
	deep := e.chainOf(t, "D1", "D2", "D3", "D4")
	sub := e.chainOf(t, "S1", "S2") // height 2
	_, err := e.update.Execute(ctx, sub[0].ID, in.UpdateCategoryCommand{ParentID: set(deep[3].ID)})
	wantViolation(t, err, "parentId", "max_depth")

	// a height-1 subtree fits exactly at depth 5
	if _, err := e.update.Execute(ctx, sub[1].ID, in.UpdateCategoryCommand{ParentID: set(deep[3].ID)}); err != nil {
		t.Errorf("leaf to depth 5: err = %v", err)
	}
}

func TestUpdateRejectsDuplicateSiblingName(t *testing.T) {
	e := newCatEnv()
	ti := e.mustCreate(t, "TI", nil)
	e.mustCreate(t, "Cloud", &ti)
	data := e.mustCreate(t, "Data", &ti)
	_, err := e.update.Execute(ctx, data.ID, in.UpdateCategoryCommand{Name: set("CLOUD")})
	wantConflict(t, err, domain.RuleDuplicateName)
}

func TestUpdateRejectsNullNameAndColor(t *testing.T) {
	e := newCatEnv()
	ti := e.mustCreate(t, "TI", nil)
	_, err := e.update.Execute(ctx, ti.ID, in.UpdateCategoryCommand{Name: setNull()})
	wantViolation(t, err, "name", "required")
	_, err = e.update.Execute(ctx, ti.ID, in.UpdateCategoryCommand{Color: setNull()})
	wantViolation(t, err, "color", "format")
}

func TestUpdateUnderMissingParent(t *testing.T) {
	e := newCatEnv()
	ti := e.mustCreate(t, "TI", nil)
	_, err := e.update.Execute(ctx, ti.ID, in.UpdateCategoryCommand{ParentID: set("00000000-0000-7000-8000-999999999999")})
	wantViolation(t, err, "parentId", "not_found")
}

// ── Delete (AC-B6, S02-5) ───────────────────────────────────────────────────

func TestDeleteBlockedByAliveChild(t *testing.T) {
	e := newCatEnv()
	c := e.chainOf(t, "TI", "Cloud")
	wantConflict(t, e.delete.Execute(ctx, c[0].ID), domain.RuleInUse)

	if err := e.delete.Execute(ctx, c[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := e.delete.Execute(ctx, c[0].ID); err != nil {
		t.Errorf("delete after child deleted: err = %v, want nil", err)
	}
}

func TestDeleteIsLogicalAndHidesTheCategory(t *testing.T) {
	e := newCatEnv()
	ti := e.mustCreate(t, "TI", nil)
	if err := e.delete.Execute(ctx, ti.ID); err != nil {
		t.Fatal(err)
	}

	row, kept := e.repo.Rows[ti.ID]
	if !kept || row.DeletedAt == nil {
		t.Fatalf("row kept=%v deletedAt=%v, want kept with deletedAt set", kept, row.DeletedAt)
	}
	_, err := e.get.Execute(ctx, ti.ID)
	wantNotFound(t, err)
	list, _ := e.list.Execute(ctx)
	if len(list) != 0 {
		t.Errorf("list = %+v, want empty", list)
	}
	wantNotFound(t, e.delete.Execute(ctx, ti.ID))
	_, err = e.update.Execute(ctx, ti.ID, in.UpdateCategoryCommand{Name: set("X")})
	wantNotFound(t, err)

	if _, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "ti", Color: "#000000"}); err != nil {
		t.Errorf("reusing a deleted name: err = %v, want nil", err)
	}
}

func TestDeleteBlockedByAliveSubject(t *testing.T) {
	e := newCatEnv()
	cat, err := e.create.Execute(ctx, in.CreateCategoryCommand{Name: "TI", Color: "#22D3EE"})
	if err != nil {
		t.Fatal(err)
	}
	name, _ := domain.NewSubjectName("Networking")
	subj := domain.NewSubject("0abcdef0-0000-7000-8000-000000000900", cat.ID, name, nil, testsupport.FixedNow)
	if err := e.subs.Create(ctx, subj); err != nil {
		t.Fatal(err)
	}

	wantKindConflict(t, e.delete.Execute(ctx, cat.ID), domain.RuleInUse)
	if v, _ := e.get.Execute(ctx, cat.ID); v.SubjectCount != 1 {
		t.Errorf("subjectCount = %d, want 1", v.SubjectCount)
	}

	subj.Delete(testsupport.FixedNow)
	if err := e.subs.Update(ctx, subj); err != nil {
		t.Fatal(err)
	}
	if err := e.delete.Execute(ctx, cat.ID); err != nil {
		t.Fatalf("delete after subject removed: %v", err)
	}
}
