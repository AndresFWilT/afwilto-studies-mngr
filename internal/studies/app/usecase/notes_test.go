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
	byTag    in.ListNotesByTag
	tags     in.ListTags
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
		byTag:    NewListNotesByTagService(repo),
		tags:     NewListTagsService(testsupport.FakeTagQuery{Notes: repo}),
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
		list, err := e.list.Execute(ctx, e.subject.ID, nil, spec)
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
	_, err := e.list.Execute(ctx, "0abcdef0-0000-7000-8000-999999999999", nil, domain.SortSpec{})
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

// ── Slice 06: tags and slab links ───────────────────────────────────────────

func (e noteEnv) addTagged(t *testing.T, title string, tags ...string) domain.NoteView {
	t.Helper()
	v, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: title, KindID: e.kind.ID, Tags: tags})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return v
}

func tagNames(v domain.NoteView) string { return strings.Join(v.Tags.Names(), ",") }

func TestCreateNoteNormalizesTags(t *testing.T) { // AC-B20
	e := newNoteEnv(t)
	v := e.addTagged(t, "IAM", " Cloud Security ", "cloud-security", "IAM")
	if got := tagNames(v); got != "cloud-security,iam" {
		t.Fatalf("tags = %s", got)
	}
	if got, _ := e.get.Execute(ctx, v.ID); tagNames(got) != "cloud-security,iam" {
		t.Fatalf("tags not persisted: %s", tagNames(got))
	}
	_, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "X", KindID: e.kind.ID, Tags: []string{"bad_tag!"}})
	wantFieldViolation(t, err, "tags[0]", "format")
}

func TestCreateNoteStoresSlabLinks(t *testing.T) { // AC-B23
	e := newNoteEnv(t)
	links := []domain.SlabLinkInput{{Label: "B-Tree lab", Path: "/slab/ud/data-structures/b-tree"}}
	v, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "B-Tree", KindID: e.kind.ID, SlabLinks: links})
	if err != nil || len(v.SlabLinks) != 1 || v.SlabLinks[0].Path != links[0].Path {
		t.Fatalf("got (%+v, %v)", v.SlabLinks, err)
	}
	_, err = e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: e.subject.ID, Title: "Bad", KindID: e.kind.ID,
		SlabLinks: []domain.SlabLinkInput{{Label: "x", Path: "/finance"}}})
	wantFieldViolation(t, err, "slabLinks[0].path", "format")
}

func TestUpdateNoteReplacesTagsAndLinksOnly(t *testing.T) { // S06-5
	e := newNoteEnv(t)
	n := e.addTagged(t, "Storage", "s3", "iam")
	links := []domain.SlabLinkInput{{Label: "l", Path: "/slab/a"}}

	got, err := e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 1,
		Tags:      in.Patch[[]string]{Set: true, Value: &[]string{"S3", "ebs"}},
		SlabLinks: in.Patch[[]domain.SlabLinkInput]{Set: true, Value: &links}})
	if err != nil || got.Version != 2 || tagNames(got) != "ebs,s3" || len(got.SlabLinks) != 1 || got.Title.String() != "Storage" {
		t.Fatalf("got (%+v, %v)", got, err)
	}
	// An unrelated edit keeps tags and links.
	got, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 2, Content: set("x")})
	if err != nil || tagNames(got) != "ebs,s3" || len(got.SlabLinks) != 1 {
		t.Fatalf("unrelated edit lost tags/links: (%+v, %v)", got, err)
	}
	// Clearing.
	empty := []string{}
	got, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 3, Tags: in.Patch[[]string]{Set: true, Value: &empty}})
	if err != nil || tagNames(got) != "" {
		t.Fatalf("clear: (%+v, %v)", got, err)
	}
	_, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 4, Tags: in.Patch[[]string]{Set: true}})
	wantFieldViolation(t, err, "tags", "required")
	_, err = e.update.Execute(ctx, n.ID, in.UpdateNoteCommand{Version: 4, SlabLinks: in.Patch[[]domain.SlabLinkInput]{Set: true}})
	wantFieldViolation(t, err, "slabLinks", "required")
}

func TestTagViewsAndCounts(t *testing.T) { // AC-B22, S06-3, S06-6
	e := newNoteEnv(t)
	other := e.addSubj(t, "Azure")
	a := e.addTagged(t, "A", "iam", "s3")
	b, err := e.create.Execute(ctx, in.CreateNoteCommand{SubjectID: other.ID, Title: "B", KindID: e.kind.ID, Tags: []string{"IAM"}})
	if err != nil {
		t.Fatal(err)
	}

	counts, _ := e.tags.Execute(ctx)
	if len(counts) != 2 || counts[0] != (domain.TagCount{Name: "iam", Count: 2}) || counts[1] != (domain.TagCount{Name: "s3", Count: 1}) {
		t.Fatalf("counts = %+v", counts)
	}
	view, err := e.byTag.Execute(ctx, " IAM ", domain.SortSpec{})
	if err != nil || len(view) != 2 {
		t.Fatalf("by tag = (%+v, %v)", view, err)
	}
	if view[0].Subject == nil || view[0].Subject.Name != "AWS" || view[0].Category == nil || view[0].Category.Path != "TI" {
		t.Fatalf("missing subject/category: %+v", view[0])
	}
	subjOnly, _ := e.list.Execute(ctx, e.subject.ID, ptr("IAM"), domain.SortSpec{})
	if len(subjOnly) != 1 || subjOnly[0].ID != a.ID || subjOnly[0].Subject != nil {
		t.Fatalf("subject filter = %+v", subjOnly)
	}

	// A deleted note and an unlinked tag stop counting.
	if err := e.delete.Execute(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.update.Execute(ctx, a.ID, in.UpdateNoteCommand{Version: 1, Tags: in.Patch[[]string]{Set: true, Value: &[]string{"s3"}}}); err != nil {
		t.Fatal(err)
	}
	counts, _ = e.tags.Execute(ctx)
	if len(counts) != 1 || counts[0] != (domain.TagCount{Name: "s3", Count: 1}) {
		t.Fatalf("counts after delete/unlink = %+v", counts)
	}
	if got, _ := e.byTag.Execute(ctx, "iam", domain.SortSpec{}); len(got) != 0 {
		t.Fatalf("iam view = %+v, want empty", got)
	}
}

func TestTagQueryValidation(t *testing.T) {
	e := newNoteEnv(t)
	_, err := e.byTag.Execute(ctx, " ", domain.SortSpec{})
	wantFieldViolation(t, err, "tag", "required")
	_, err = e.byTag.Execute(ctx, "bad_tag!", domain.SortSpec{})
	wantFieldViolation(t, err, "tag", "format")
	_, err = e.list.Execute(ctx, e.subject.ID, ptr("bad_tag!"), domain.SortSpec{})
	wantFieldViolation(t, err, "tag", "format")
}
