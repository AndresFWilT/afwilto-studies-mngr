package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

type subjEnv struct {
	cats   *testsupport.FakeCategoryRepo
	repo   *testsupport.FakeSubjectRepo
	create in.CreateSubject
	get    in.GetSubject
	list   in.ListSubjects
	update in.UpdateSubject
	delete in.DeleteSubject
	addCat func(t *testing.T, name string) domain.CategoryView
}

func newSubjEnv() subjEnv {
	cats := testsupport.NewFakeCategoryRepo()
	repo := testsupport.NewFakeSubjectRepo(cats)
	notes := testsupport.NewFakeNoteRepo(repo, testsupport.NewFakeNoteKindRepo())
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	createCat := NewCreateCategoryService(cats, clock, ids)
	return subjEnv{
		cats:   cats,
		repo:   repo,
		create: NewCreateSubjectService(repo, cats, clock, ids),
		get:    NewGetSubjectService(repo),
		list:   NewListSubjectsService(repo),
		update: NewUpdateSubjectService(repo, cats, clock),
		delete: NewDeleteSubjectService(repo, notes, clock),
		addCat: func(t *testing.T, name string) domain.CategoryView {
			t.Helper()
			v, err := createCat.Execute(ctx, in.CreateCategoryCommand{Name: name, Color: "#22D3EE"})
			if err != nil {
				t.Fatal(err)
			}
			return v
		},
	}
}

func (e subjEnv) add(t *testing.T, categoryID, name string) domain.SubjectView {
	t.Helper()
	v, err := e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: categoryID, Name: name})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return v
}

func wantFieldViolation(t *testing.T, err error, field, rule string) {
	t.Helper()
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Violations[0].Field != field || v.Violations[0].Rule != rule {
		t.Fatalf("err = %v, want %s/%s violation", err, field, rule)
	}
}

func TestCreateSubject(t *testing.T) {
	e := newSubjEnv()
	ti := e.addCat(t, "TI")
	desc := "about networks"
	v, err := e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: ti.ID, Name: " Networking ", Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if v.Name.String() != "Networking" || *v.Description != desc || v.Category.Path != "TI" || v.NoteCount != 0 {
		t.Fatalf("got %+v", v)
	}
}

func TestCreateSubjectInNonLeafCategory(t *testing.T) { // AC-B12
	e := newSubjEnv()
	ti := e.addCat(t, "TI")
	cloud, _ := NewCreateCategoryService(e.cats, &testsupport.StepClock{}, &testsupport.SeqIDGen{N: 500}).
		Execute(ctx, in.CreateCategoryCommand{Name: "Cloud", Color: "#000000", ParentID: &ti.ID})
	_ = cloud
	e.add(t, ti.ID, "Certifications")
}

func TestCreateSubjectNameRules(t *testing.T) { // AC-B11
	e := newSubjEnv()
	a, b := e.addCat(t, "A"), e.addCat(t, "B")
	e.add(t, a.ID, "Networking")
	e.add(t, b.ID, "Networking") // other category: fine
	_, err := e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: a.ID, Name: "NETWORKING"})
	wantKindConflict(t, err, domain.RuleDuplicateName)
}

func TestCreateSubjectValidation(t *testing.T) {
	e := newSubjEnv()
	a := e.addCat(t, "A")
	_, err := e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: a.ID, Name: " "})
	wantFieldViolation(t, err, "name", "required")
	long := strings.Repeat("x", 2001)
	_, err = e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: a.ID, Name: "N", Description: &long})
	wantFieldViolation(t, err, "description", "max_length")
}

func TestCreateSubjectUnknownOrDeletedCategory(t *testing.T) { // AC-B8
	e := newSubjEnv()
	_, err := e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: "0abcdef0-0000-7000-8000-999999999999", Name: "N"})
	wantFieldViolation(t, err, "categoryId", "not_found")

	a := e.addCat(t, "A")
	if err := NewDeleteCategoryService(e.cats, e.repo, &testsupport.StepClock{}).Execute(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.create.Execute(ctx, in.CreateSubjectCommand{CategoryID: a.ID, Name: "N"})
	wantFieldViolation(t, err, "categoryId", "not_found")
}

func TestListSubjectsFiltersDirectMembersAndSorts(t *testing.T) {
	e := newSubjEnv()
	a, b := e.addCat(t, "A"), e.addCat(t, "B")
	e.add(t, a.ID, "zeta")
	e.add(t, a.ID, "Alpha")
	e.add(t, b.ID, "Other")

	all, _ := e.list.Execute(ctx, nil)
	if len(all) != 3 {
		t.Fatalf("all = %d, want 3", len(all))
	}
	got, _ := e.list.Execute(ctx, &a.ID)
	if len(got) != 2 || got[0].Name.String() != "Alpha" || got[1].Name.String() != "zeta" {
		t.Fatalf("filtered = %+v", got)
	}
	unknown := "0abcdef0-0000-7000-8000-999999999999"
	if got, err := e.list.Execute(ctx, &unknown); err != nil || len(got) != 0 {
		t.Fatalf("unknown category = (%v, %v), want empty", got, err)
	}
}

func TestUpdateSubjectRenameRedescribeAndMove(t *testing.T) {
	e := newSubjEnv()
	a, b := e.addCat(t, "A"), e.addCat(t, "B")
	s := e.add(t, a.ID, "Net")

	got, err := e.update.Execute(ctx, s.ID, in.UpdateSubjectCommand{
		Name: set("Networking"), Description: set("d"), CategoryID: set(b.ID),
	})
	if err != nil || got.Name.String() != "Networking" || *got.Description != "d" || got.Category.ID != b.ID {
		t.Fatalf("got (%+v, %v)", got, err)
	}
	got, err = e.update.Execute(ctx, s.ID, in.UpdateSubjectCommand{Description: in.Patch[string]{Set: true}})
	if err != nil || got.Description != nil {
		t.Fatalf("null description: got (%+v, %v)", got, err)
	}
}

func TestUpdateSubjectValidation(t *testing.T) {
	e := newSubjEnv()
	a := e.addCat(t, "A")
	s := e.add(t, a.ID, "Net")
	null := in.Patch[string]{Set: true}

	_, err := e.update.Execute(ctx, s.ID, in.UpdateSubjectCommand{Name: null})
	wantFieldViolation(t, err, "name", "required")
	_, err = e.update.Execute(ctx, s.ID, in.UpdateSubjectCommand{CategoryID: null})
	wantFieldViolation(t, err, "categoryId", "required")
	_, err = e.update.Execute(ctx, s.ID, in.UpdateSubjectCommand{CategoryID: set("0abcdef0-0000-7000-8000-999999999999")})
	wantFieldViolation(t, err, "categoryId", "not_found")
}

func TestUpdateSubjectMoveOntoClashingName(t *testing.T) {
	e := newSubjEnv()
	a, b := e.addCat(t, "A"), e.addCat(t, "B")
	s := e.add(t, a.ID, "Net")
	e.add(t, b.ID, "net")
	_, err := e.update.Execute(ctx, s.ID, in.UpdateSubjectCommand{CategoryID: set(b.ID)})
	wantKindConflict(t, err, domain.RuleDuplicateName)
}

func TestDeleteSubjectIsLogicalAndFreesTheName(t *testing.T) {
	e := newSubjEnv()
	a := e.addCat(t, "A")
	s := e.add(t, a.ID, "Net")
	if err := e.delete.Execute(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if e.repo.Rows[s.ID].DeletedAt == nil {
		t.Fatal("row must remain with deletedAt")
	}
	if _, err := e.get.Execute(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get = %v, want ErrNotFound", err)
	}
	if err := e.delete.Execute(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	e.add(t, a.ID, "net")
}
