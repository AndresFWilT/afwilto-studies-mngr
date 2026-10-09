package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

type noteEnv struct {
	clock    *testsupport.StepClock
	cats     *testsupport.FakeCategoryRepo
	subjects *testsupport.FakeSubjectRepo
	kinds    *testsupport.FakeNoteKindRepo
	repo     *testsupport.FakeNoteRepo

	create   in.CreateNote
	get      in.GetNote
	list     in.ListSubjectNotes
	update   in.UpdateNote
	delete   in.DeleteNote
	delSubj  in.DeleteSubject
	delKind  in.DeleteNoteKind
	subject  domain.SubjectView
	kind     domain.NoteKindView
	addKind  func(t *testing.T, name string) domain.NoteKindView
	addSubj  func(t *testing.T, name string) domain.SubjectView
	getSubj  in.GetSubject
	listKind in.ListNoteKinds
}

func newNoteEnv(t *testing.T) noteEnv {
	t.Helper()
	cats := testsupport.NewFakeCategoryRepo()
	subjects := testsupport.NewFakeSubjectRepo(cats)
	kinds := testsupport.NewFakeNoteKindRepo()
	repo := testsupport.NewFakeNoteRepo(subjects, kinds)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}

	cat, err := NewCreateCategoryService(cats, clock, ids).Execute(ctx, in.CreateCategoryCommand{Name: "TI", Color: "#22D3EE"})
	if err != nil {
		t.Fatal(err)
	}
	createSubj := NewCreateSubjectService(subjects, cats, clock, ids)
	createKind := NewCreateNoteKindService(kinds, clock, ids)
	e := noteEnv{
		clock: clock, cats: cats, subjects: subjects, kinds: kinds, repo: repo,
		create:   NewCreateNoteService(repo, subjects, kinds, clock, ids),
		get:      NewGetNoteService(repo),
		list:     NewListSubjectNotesService(repo, subjects),
		update:   NewUpdateNoteService(repo, subjects, kinds, clock),
		delete:   NewDeleteNoteService(repo, clock),
		delSubj:  NewDeleteSubjectService(subjects, repo, clock),
		delKind:  NewDeleteNoteKindService(kinds, repo, clock),
		getSubj:  NewGetSubjectService(subjects),
		listKind: NewListNoteKindsService(kinds),
	}
	e.addKind = func(t *testing.T, name string) domain.NoteKindView {
		t.Helper()
		v, err := createKind.Execute(ctx, in.CreateNoteKindCommand{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	e.addSubj = func(t *testing.T, name string) domain.SubjectView {
		t.Helper()
		v, err := createSubj.Execute(ctx, in.CreateSubjectCommand{CategoryID: cat.ID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	e.subject = e.addSubj(t, "AWS")
	e.kind = e.addKind(t, "Notes")
	return e
}

func (e noteEnv) add(t *testing.T, title string) domain.NoteView {
	t.Helper()
	v, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: title, KindID: e.kind.ID})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return v
}

func TestCreateNoteStoresContentVerbatim(t *testing.T) { // AC-B15
	e := newNoteEnv(t)
	content := "$$E=mc^2$$\n```go\nx := 1\n```\n```mermaid\ngraph TD; A-->B\n```\r\n  "
	v, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: " Storage ", KindID: e.kind.ID, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if v.Title.String() != "Storage" || v.Content.String() != content || v.Version != 1 || v.Kind.Name != "Notes" || v.Kind.ID != e.kind.ID {
		t.Fatalf("got %+v", v)
	}
	got, _ := e.get.Execute(ctx, v.ID)
	if got.Content.String() != content {
		t.Fatal("content changed on read")
	}
}

func TestCreateNoteDefaultsToEmptyContent(t *testing.T) {
	e := newNoteEnv(t)
	if v := e.add(t, "Empty"); v.Content.String() != "" {
		t.Fatalf("content = %q", v.Content.String())
	}
}

func TestCreateNoteContentBoundary(t *testing.T) { // AC-B18
	e := newNoteEnv(t)
	ok, tooBig := strings.Repeat("a", 200000), strings.Repeat("a", 200001)
	if _, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "ok", KindID: e.kind.ID, Content: &ok}); err != nil {
		t.Fatalf("200000: %v", err)
	}
	_, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "big", KindID: e.kind.ID, Content: &tooBig})
	wantFieldViolation(t, err, "content", "max_length")
}

func TestCreateNoteReferences(t *testing.T) { // BR-DEL2
	e := newNoteEnv(t)
	_, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: "0abcdef0-0000-7000-8000-999999999999", Title: "T", KindID: e.kind.ID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown subject = %v, want ErrNotFound", err)
	}
	_, err = e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "T", KindID: "0abcdef0-0000-7000-8000-999999999999"})
	wantFieldViolation(t, err, "kindId", "not_found")

	gone := e.addKind(t, "Gone")
	if err := e.delKind.Execute(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "T", KindID: gone.ID})
	wantFieldViolation(t, err, "kindId", "not_found")
}

func TestCreateNoteTitleRules(t *testing.T) { // BR-NOTE1
	e := newNoteEnv(t)
	e.add(t, "Storage")
	_, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "STORAGE", KindID: e.kind.ID})
	wantKindConflict(t, err, domain.RuleDuplicateName)

	other := e.addSubj(t, "Azure")
	if _, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: other.ID, Title: "Storage", KindID: e.kind.ID}); err != nil {
		t.Fatalf("same title in another subject: %v", err)
	}
	_, err = e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: " ", KindID: e.kind.ID})
	wantFieldViolation(t, err, "title", "required")
}

func TestListSubjectNotesSorting(t *testing.T) { // AC-B16
	e := newNoteEnv(t)
	// created: Compute < Storage < Networking; updated: Networking < Storage < Compute
	compute, storage, networking := e.add(t, "Compute"), e.add(t, "Storage"), e.add(t, "Networking")
	// StepClock advances per call, so touching in this order sets the updated order.
	touch := func(id string) {
		if _, err := e.update.Execute(ctx, id, in.UpdateNoteCommand{Version: 1, Content: set("x")}); err != nil {
			t.Fatal(err)
		}
	}
	touch(networking.ID)
	touch(storage.ID)
	touch(compute.ID)

	order := func(sort, ord string) string {
		spec, err := domain.ParseSortSpec(sort, ord)
		if err != nil {
			t.Fatal(err)
		}
		list, err := e.list.Execute(ctx, e.subject.ID, spec)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, v := range list {
			if v.Content.String() != "" {
				t.Fatal("list items must not carry content")
			}
			names = append(names, v.Title.String())
		}
		return strings.Join(names, ",")
	}
	tests := []struct{ sort, order, want string }{
		{"", "", "Compute,Networking,Storage"},
		{"title", "desc", "Storage,Networking,Compute"},
		{"updatedAt", "desc", "Compute,Storage,Networking"},
		{"createdAt", "desc", "Networking,Storage,Compute"},
		{"createdAt", "asc", "Compute,Storage,Networking"},
	}
	for _, tc := range tests {
		if got := order(tc.sort, tc.order); got != tc.want {
			t.Errorf("sort=%q order=%q: got %s, want %s", tc.sort, tc.order, got, tc.want)
		}
	}
}

func TestListSubjectNotesOfMissingSubject(t *testing.T) {
	e := newNoteEnv(t)
	_, err := e.list.Execute(ctx, "0abcdef0-0000-7000-8000-999999999999", domain.SortSpec{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateNoteVersionGuard(t *testing.T) { // AC-B17
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	before := n.UpdatedAt

	got, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, Content: set("v2")})
	if err != nil || got.Version != 2 || got.Content.String() != "v2" || !got.UpdatedAt.After(before) {
		t.Fatalf("first edit = (%+v, %v)", got, err)
	}
	_, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, Content: set("lost")})
	wantKindConflict(t, err, domain.RuleStaleVersion)
	if cur, _ := e.get.Execute(ctx, n.ID); cur.Content.String() != "v2" || cur.Version != 2 {
		t.Fatalf("stale edit changed the note: %+v", cur)
	}
}

func TestUpdateNoteRequiresVersionAndAChange(t *testing.T) {
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	_, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 0, Title: set("X")})
	wantFieldViolation(t, err, "version", "required")
	_, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1})
	wantFieldViolation(t, err, "body", "required")
}

func TestUpdateNoteFieldsAndNulls(t *testing.T) {
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	other := e.addKind(t, "Summary")
	null := in.Patch[string]{Set: true}

	got, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, Title: set("Block storage"), KindID: set(other.ID)})
	if err != nil || got.Title.String() != "Block storage" || got.Kind.Name != "Summary" {
		t.Fatalf("got (%+v, %v)", got, err)
	}
	for field, cmd := range map[string]in.UpdateNoteCommand{
		"title":     {Version: 2, Title: null},
		"kindId":    {Version: 2, KindID: null},
		"subjectId": {Version: 2, SubjectID: null},
		"content":   {Version: 2, Content: null},
	} {
		_, err := e.update.Execute(ctx, n.ID, cmd)
		wantFieldViolation(t, err, field, "required")
	}
}

func TestUpdateNoteMovesToAnotherSubject(t *testing.T) {
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	other := e.addSubj(t, "Azure")
	if _, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: other.ID, Title: "storage", KindID: e.kind.ID}); err != nil {
		t.Fatal(err)
	}
	_, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, SubjectID: set(other.ID)})
	wantKindConflict(t, err, domain.RuleDuplicateName)

	third := e.addSubj(t, "GCP")
	got, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, SubjectID: set(third.ID)})
	if err != nil || got.SubjectID != third.ID {
		t.Fatalf("move = (%+v, %v)", got, err)
	}

	_, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 2, SubjectID: set("0abcdef0-0000-7000-8000-999999999999")})
	wantFieldViolation(t, err, "subjectId", "not_found")
	if err := e.delSubj.Execute(ctx, e.subject.ID); err != nil { // emptied by the move
		t.Fatal(err)
	}
	_, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 2, SubjectID: set(e.subject.ID)})
	wantFieldViolation(t, err, "subjectId", "not_found")
}

func TestUpdateStaleBeatsValidation(t *testing.T) {
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	if _, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, Content: set("a")}); err != nil {
		t.Fatal(err)
	}
	_, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, Title: set(" ")})
	wantKindConflict(t, err, domain.RuleStaleVersion)
}

func TestDeleteNoteIsLogicalAndFreesTitle(t *testing.T) { // AC-B19
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	if got, _ := e.getSubj.Execute(ctx, e.subject.ID); got.NoteCount != 1 {
		t.Fatalf("noteCount = %d, want 1", got.NoteCount)
	}
	if err := e.delete.Execute(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if e.repo.Rows[n.ID].DeletedAt == nil {
		t.Fatal("row must remain with deletedAt")
	}
	if _, err := e.get.Execute(ctx, n.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get = %v", err)
	}
	if _, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1, Title: set("X")}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update = %v", err)
	}
	if err := e.delete.Execute(ctx, n.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	if got, _ := e.getSubj.Execute(ctx, e.subject.ID); got.NoteCount != 0 {
		t.Fatalf("noteCount after delete = %d", got.NoteCount)
	}
	e.add(t, "storage") // title reusable
}

func TestSubjectAndKindDeleteBlockedByAliveNotes(t *testing.T) { // BR-DEL1
	e := newNoteEnv(t)
	n := e.add(t, "Storage")
	wantKindConflict(t, e.delSubj.Execute(ctx, e.subject.ID), domain.RuleInUse)
	wantKindConflict(t, e.delKind.Execute(ctx, e.kind.ID), domain.RuleInUse)

	kinds, _ := e.listKind.Execute(ctx)
	for _, k := range kinds {
		if k.ID == e.kind.ID && k.NoteCount != 1 {
			t.Errorf("kind noteCount = %d, want 1", k.NoteCount)
		}
	}
	if err := e.delete.Execute(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.delSubj.Execute(ctx, e.subject.ID); err != nil {
		t.Fatalf("subject delete after note removed: %v", err)
	}
	if err := e.delKind.Execute(ctx, e.kind.ID); err != nil {
		t.Fatalf("kind delete after note removed: %v", err)
	}
}
