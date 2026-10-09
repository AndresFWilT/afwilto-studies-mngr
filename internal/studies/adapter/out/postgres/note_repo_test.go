package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

type noteRepoEnv struct {
	subj    *subjRepoEnv
	kinds   *NoteKindRepository
	repo    *NoteRepository
	subject domain.Subject
	kind    domain.NoteKind
	ids     *testsupport.SeqIDGen
	now     time.Time
}

func newNoteRepoEnv(t *testing.T) *noteRepoEnv {
	t.Helper()
	subj := newSubjRepoEnv(t) // truncates categories CASCADE (subjects, notes)
	exec(t, `TRUNCATE note_kinds CASCADE`)
	cat := subj.cats.add(t, "TI", nil)
	e := &noteRepoEnv{
		subj: subj, kinds: NewNoteKindRepository(pool), repo: NewNoteRepository(pool),
		ids: &testsupport.SeqIDGen{N: 5000}, now: testsupport.FixedNow,
	}
	e.subject = subj.add(t, cat.ID, "AWS")
	e.kind = e.addKind(t, "Notes")
	return e
}

func (e *noteRepoEnv) addKind(t *testing.T, name string) domain.NoteKind {
	t.Helper()
	n, err := domain.NewNoteKindName(name)
	if err != nil {
		t.Fatal(err)
	}
	k := domain.NewNoteKind(e.ids.NewID(), n, e.now)
	if err := e.kinds.Create(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	return k
}

func (e *noteRepoEnv) note(t *testing.T, subjectID, title, content string) domain.Note {
	t.Helper()
	ti, err := domain.NewNoteTitle(title)
	if err != nil {
		t.Fatal(err)
	}
	c, err := domain.NewMarkdownContent(content)
	if err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	return domain.NewNote(e.ids.NewID(), subjectID, e.kind.ID, ti, c, e.now)
}

func (e *noteRepoEnv) add(t *testing.T, title string) domain.Note {
	t.Helper()
	n := e.note(t, e.subject.ID, title, "")
	if err := e.repo.Create(context.Background(), n); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return n
}

func TestNoteRepoContentRoundTripsByteForByte(t *testing.T) { // AC-B15
	e := newNoteRepoEnv(t)
	content := "$$E=mc^2$$\n```go\nx := \"ñ\"\n```\r\n```mermaid\ngraph TD; A-->B\n```\n  \t trailing  "
	n := e.note(t, e.subject.ID, "Storage", content)
	if err := e.repo.Create(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	v, err := e.repo.GetView(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Content.String() != content || v.Version != 1 || v.Kind.ID != e.kind.ID || v.Kind.Name != "Notes" || v.Title.String() != "Storage" {
		t.Fatalf("view = %+v", v)
	}
}

func TestNoteRepoContentLimitBoundary(t *testing.T) { // AC-B18
	e := newNoteRepoEnv(t)
	n := e.note(t, e.subject.ID, "Big", strings.Repeat("ñ", 200000))
	if err := e.repo.Create(context.Background(), n); err != nil {
		t.Fatalf("200000 multibyte characters: %v", err)
	}
}

func TestNoteRepoTitleUniquePerSubjectAmongAlive(t *testing.T) { // BR-NOTE1
	e := newNoteRepoEnv(t)
	n := e.add(t, "Storage")
	other := e.subj.add(t, e.subject.CategoryID, "Azure")
	if err := e.repo.Create(context.Background(), e.note(t, other.ID, "Storage", "")); err != nil {
		t.Fatalf("same title, other subject: %v", err)
	}
	wantConflictErr(t, e.repo.Create(context.Background(), e.note(t, e.subject.ID, "STORAGE", "")), domain.RuleDuplicateName)

	n.Delete(e.now.Add(time.Hour))
	if err := e.repo.Delete(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	e.add(t, "Storage") // freed
}

func TestNoteRepoStaleVersionUpdate(t *testing.T) { // AC-B17
	e := newNoteRepoEnv(t)
	n := e.add(t, "Storage")
	ctx := context.Background()

	n.Content = domain.RehydrateMarkdownContent("v2")
	n.Touch(e.now.Add(time.Minute))
	if err := e.repo.Update(ctx, n, 1); err != nil {
		t.Fatal(err)
	}
	stale := n
	stale.Content = domain.RehydrateMarkdownContent("lost")
	stale.Version = 2
	wantConflictErr(t, e.repo.Update(ctx, stale, 1), domain.RuleStaleVersion)

	v, _ := e.repo.GetView(ctx, n.ID)
	if v.Content.String() != "v2" || v.Version != 2 {
		t.Fatalf("stale update changed the row: %+v", v)
	}

	n.Delete(e.now.Add(time.Hour))
	if err := e.repo.Delete(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Update(ctx, n, 2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update of deleted = %v, want ErrNotFound", err)
	}
}

func TestNoteRepoListSorting(t *testing.T) { // AC-B16
	e := newNoteRepoEnv(t)
	compute, storage, networking := e.add(t, "Compute"), e.add(t, "Storage"), e.add(t, "Networking")
	ctx := context.Background()
	for _, n := range []domain.Note{networking, storage, compute} { // updated order
		n.Touch(e.now.Add(time.Hour))
		e.now = e.now.Add(time.Minute)
		n.UpdatedAt = e.now
		if err := e.repo.Update(ctx, n, 1); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		sort, order, want string
	}{
		{"", "", "Compute,Networking,Storage"},
		{"title", "desc", "Storage,Networking,Compute"},
		{"updatedAt", "desc", "Compute,Storage,Networking"},
		{"createdAt", "desc", "Networking,Storage,Compute"},
		{"createdAt", "asc", "Compute,Storage,Networking"},
	}
	for _, tc := range tests {
		spec, _ := domain.ParseSortSpec(tc.sort, tc.order)
		list, err := e.repo.ListBySubject(ctx, e.subject.ID, spec)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, v := range list {
			names = append(names, v.Title.String())
			if v.Content.String() != "" {
				t.Errorf("list item carries content")
			}
		}
		if got := strings.Join(names, ","); got != tc.want {
			t.Errorf("sort=%q order=%q: %s, want %s", tc.sort, tc.order, got, tc.want)
		}
	}
}

func TestNoteRepoTiesBreakByTitleThenID(t *testing.T) {
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	b, a := e.note(t, e.subject.ID, "b", ""), e.note(t, e.subject.ID, "a", "")
	b.CreatedAt, a.CreatedAt = testsupport.FixedNow, testsupport.FixedNow
	for _, n := range []domain.Note{b, a} {
		if err := e.repo.Create(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	spec, _ := domain.ParseSortSpec("createdAt", "desc")
	list, _ := e.repo.ListBySubject(ctx, e.subject.ID, spec)
	if list[0].Title.String() != "a" || list[1].Title.String() != "b" {
		t.Fatalf("tie order = %s, %s; want a, b", list[0].Title.String(), list[1].Title.String())
	}
}

func TestNoteCountsAndDeleteGuards(t *testing.T) { // BR-DEL1, S05-7, S05-12
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	n := e.add(t, "Storage")

	sv, _ := e.subj.repo.GetView(ctx, e.subject.ID)
	kinds, _ := e.kinds.List(ctx)
	if sv.NoteCount != 1 || len(kinds) != 1 || kinds[0].NoteCount != 1 {
		t.Fatalf("counts: subject %d, kinds %+v", sv.NoteCount, kinds)
	}
	if c, _ := e.repo.CountAliveBySubject(ctx, e.subject.ID); c != 1 {
		t.Errorf("CountAliveBySubject = %d", c)
	}
	if c, _ := e.repo.CountAliveByKind(ctx, e.kind.ID); c != 1 {
		t.Errorf("CountAliveByKind = %d", c)
	}

	subj := e.subject
	subj.Delete(e.now)
	wantConflictErr(t, e.subj.repo.Update(ctx, subj), domain.RuleInUse)
	kind := e.kind
	kind.Delete(e.now)
	wantConflictErr(t, e.kinds.Update(ctx, kind), domain.RuleInUse)

	n.Delete(e.now)
	if err := e.repo.Delete(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := e.kinds.Update(ctx, kind); err != nil {
		t.Fatalf("kind delete after note removed: %v", err)
	}
}

func TestNotesRejectMissingOrDeletedRefs(t *testing.T) { // S05-12
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	var v *domain.ValidationError

	missing := e.note(t, "0abcdef0-0000-7000-8000-999999999999", "N", "")
	if err := e.repo.Create(ctx, missing); !errors.As(err, &v) || v.Violations[0].Field != "subjectId" {
		t.Fatalf("missing subject: %v", err)
	}
	gone := e.addKind(t, "Gone")
	gone.Delete(e.now)
	if err := e.kinds.Update(ctx, gone); err != nil {
		t.Fatal(err)
	}
	n := e.note(t, e.subject.ID, "N", "")
	n.KindID = gone.ID
	if err := e.repo.Create(ctx, n); !errors.As(err, &v) || v.Violations[0].Field != "kindId" || v.Violations[0].Rule != "not_found" {
		t.Fatalf("deleted kind on create: %v", err)
	}

	ok := e.add(t, "Moves")
	ok.KindID = gone.ID
	ok.Touch(e.now)
	if err := e.repo.Update(ctx, ok, 1); !errors.As(err, &v) || v.Violations[0].Field != "kindId" {
		t.Fatalf("deleted kind on update: %v", err)
	}
}

func TestNoteLogicalDeleteKeepsRowAndVersion(t *testing.T) { // AC-B19
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	n := e.add(t, "Storage")
	n.Delete(e.now.Add(time.Hour))
	if err := e.repo.Delete(ctx, n); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.Get(ctx, n.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get = %v", err)
	}
	if err := e.repo.Delete(ctx, n); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second Delete = %v", err)
	}
	var stamped bool
	var version int
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL, version FROM notes WHERE id=$1`, n.ID).Scan(&stamped, &version); err != nil || !stamped || version != 1 {
		t.Fatalf("row: stamped=%v version=%d err=%v", stamped, version, err)
	}
}

func TestNotesNoHardDeleteTrigger(t *testing.T) { // S05-14
	e := newNoteRepoEnv(t)
	e.add(t, "Storage")
	_, err := pool.Exec(context.Background(), `DELETE FROM notes`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
		t.Fatalf("err = %v, want SQLSTATE 23001", err)
	}
}

func TestNoteRepoConcurrentEditsExactlyOneWins(t *testing.T) { // S05-3 under concurrency
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	n := e.add(t, "Storage")

	const writers = 8
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func() {
			edit := n
			edit.Content = domain.RehydrateMarkdownContent("edit")
			edit.Touch(e.now.Add(time.Minute))
			results <- e.repo.Update(ctx, edit, 1)
		}()
	}
	wins, stale := 0, 0
	for i := 0; i < writers; i++ {
		err := <-results
		var c *domain.ConflictError
		switch {
		case err == nil:
			wins++
		case errors.As(err, &c) && c.Rule == domain.RuleStaleVersion:
			stale++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || stale != writers-1 {
		t.Fatalf("wins=%d stale=%d, want 1 and %d", wins, stale, writers-1)
	}
	if v, _ := e.repo.GetView(ctx, n.ID); v.Version != 2 {
		t.Fatalf("version = %d, want 2", v.Version)
	}
}
