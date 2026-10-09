package usecase

import (
	"errors"
	"testing"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

type kindEnv struct {
	repo   *testsupport.FakeNoteKindRepo
	create in.CreateNoteKind
	get    in.GetNoteKind
	list   in.ListNoteKinds
	rename in.RenameNoteKind
	delete in.DeleteNoteKind
}

func newKindEnv() kindEnv {
	repo := testsupport.NewFakeNoteKindRepo()
	notes := testsupport.NewFakeNoteRepo(testsupport.NewFakeSubjectRepo(testsupport.NewFakeCategoryRepo()), repo)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	return kindEnv{
		repo:   repo,
		create: NewCreateNoteKindService(repo, clock, ids),
		get:    NewGetNoteKindService(repo),
		list:   NewListNoteKindsService(repo),
		rename: NewRenameNoteKindService(repo, clock),
		delete: NewDeleteNoteKindService(repo, notes, clock),
	}
}

func (e kindEnv) add(t *testing.T, name string) domain.NoteKindView {
	t.Helper()
	v, err := e.create.Execute(ctx, in.CreateNoteKindCommand{Name: name})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return v
}

func wantKindConflict(t *testing.T, err error, rule string) {
	t.Helper()
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != rule {
		t.Fatalf("err = %v, want ConflictError(%s)", err, rule)
	}
}

func TestCreateNoteKind(t *testing.T) {
	e := newKindEnv()
	v := e.add(t, "  Lab report ")
	if v.Name.String() != "Lab report" || v.NoteCount != 0 {
		t.Fatalf("got %+v", v)
	}
}

func TestCreateNoteKindRejectsInvalidName(t *testing.T) {
	_, err := newKindEnv().create.Execute(ctx, in.CreateNoteKindCommand{Name: " "})
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Violations[0].Rule != "required" {
		t.Fatalf("err = %v, want required violation", err)
	}
}

func TestCreateNoteKindDuplicateIsCaseInsensitive(t *testing.T) {
	e := newKindEnv()
	e.add(t, "Lab report")
	_, err := e.create.Execute(ctx, in.CreateNoteKindCommand{Name: "lab REPORT"})
	wantKindConflict(t, err, domain.RuleDuplicateName)
}

func TestListNoteKindsIsAlphabetical(t *testing.T) {
	e := newKindEnv()
	for _, n := range []string{"Summary", "cheat sheet", "Notes"} {
		e.add(t, n)
	}
	got, err := e.list.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cheat sheet", "Notes", "Summary"}
	for i, v := range got {
		if v.Name.String() != want[i] {
			t.Fatalf("order = %v at %d, want %v", v.Name.String(), i, want)
		}
	}
}

func TestRenameNoteKind(t *testing.T) {
	e := newKindEnv()
	k := e.add(t, "Notes")
	got, err := e.rename.Execute(ctx, k.ID, in.RenameNoteKindCommand{Name: "Study notes"})
	if err != nil || got.Name.String() != "Study notes" {
		t.Fatalf("got (%+v, %v)", got, err)
	}
}

func TestRenameNoteKindToItsOwnNameDifferingInCaseSucceeds(t *testing.T) {
	e := newKindEnv()
	k := e.add(t, "Notes")
	got, err := e.rename.Execute(ctx, k.ID, in.RenameNoteKindCommand{Name: "NOTES"})
	if err != nil || got.Name.String() != "NOTES" {
		t.Fatalf("got (%+v, %v)", got, err)
	}
}

func TestRenameNoteKindToAnotherKindsNameConflicts(t *testing.T) {
	e := newKindEnv()
	e.add(t, "Summary")
	k := e.add(t, "Notes")
	_, err := e.rename.Execute(ctx, k.ID, in.RenameNoteKindCommand{Name: "summary"})
	wantKindConflict(t, err, domain.RuleDuplicateName)
}

func TestRenameNoteKindToDeletedKindsNameSucceeds(t *testing.T) {
	e := newKindEnv()
	gone := e.add(t, "Summary")
	k := e.add(t, "Notes")
	if err := e.delete.Execute(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rename.Execute(ctx, k.ID, in.RenameNoteKindCommand{Name: "Summary"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func TestDeleteNoteKindIsLogicalAndFreesTheName(t *testing.T) {
	e := newKindEnv()
	k := e.add(t, "Notes")
	if err := e.delete.Execute(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if row := e.repo.Rows[k.ID]; row.DeletedAt == nil {
		t.Fatal("row must remain with deletedAt set")
	}
	if _, err := e.get.Execute(ctx, k.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	if err := e.delete.Execute(ctx, k.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
	if _, err := e.rename.Execute(ctx, k.ID, in.RenameNoteKindCommand{Name: "X"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rename after delete = %v, want ErrNotFound", err)
	}
	e.add(t, "notes") // name reusable
}
