package postgres

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

func (e *noteRepoEnv) tagged(t *testing.T, title string, tags ...string) domain.Note {
	t.Helper()
	n := e.note(t, e.subject.ID, title, "")
	set, err := domain.NewTagSet(tags)
	if err != nil {
		t.Fatal(err)
	}
	n.Tags = set
	if err := e.repo.Create(context.Background(), n); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return n
}

func (e *noteRepoEnv) retag(t *testing.T, n domain.Note, tags ...string) domain.Note {
	t.Helper()
	cur, err := e.repo.Get(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	set, err := domain.NewTagSet(tags)
	if err != nil {
		t.Fatal(err)
	}
	cur.Tags = set
	expected := cur.Version
	e.now = e.now.Add(time.Minute)
	cur.Touch(e.now)
	if err := e.repo.Update(context.Background(), cur, expected); err != nil {
		t.Fatalf("retag: %v", err)
	}
	return cur
}

func TestNoteRepoTagsRoundTripSorted(t *testing.T) { // AC-B20
	e := newNoteRepoEnv(t)
	n := e.tagged(t, "IAM", " Cloud Security ", "cloud-security", "IAM")

	v, err := e.repo.GetView(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Tags.Names(); !slices.Equal(got, []string{"cloud-security", "iam"}) {
		t.Fatalf("tags = %v", got)
	}
	list, _ := e.repo.ListBySubject(context.Background(), e.subject.ID, nil, domain.SortSpec{})
	if got := list[0].Tags.Names(); !slices.Equal(got, []string{"cloud-security", "iam"}) {
		t.Fatalf("summary tags = %v", got)
	}
	if got, _ := e.repo.Get(context.Background(), n.ID); !slices.Equal(got.Tags.Names(), []string{"cloud-security", "iam"}) {
		t.Fatalf("Get must load tags so an edit keeps them, got %v", got.Tags.Names())
	}
}

func TestNoteRepoUnlinkAndReviveKeepsTheRow(t *testing.T) { // AC-B21
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	n := e.tagged(t, "IAM", "iam", "s3")

	type link struct {
		createdAt time.Time
		deleted   bool
	}
	read := func() link {
		var l link
		if err := pool.QueryRow(ctx, `
			SELECT nt.created_at, nt.deleted_at IS NOT NULL
			FROM note_tags nt JOIN tags t ON t.id = nt.tag_id
			WHERE nt.note_id = $1 AND t.name = 'iam'`, n.ID).Scan(&l.createdAt, &l.deleted); err != nil {
			t.Fatal(err)
		}
		return l
	}
	before := read()

	e.retag(t, n, "s3")
	dropped := read() // the row is still there
	if !dropped.deleted || !dropped.createdAt.Equal(before.createdAt) {
		t.Fatalf("after drop: %+v (before %+v)", dropped, before)
	}
	if views, _ := e.repo.ListByTag(ctx, "iam", domain.SortSpec{}); len(views) != 0 {
		t.Fatalf("note still in the iam view: %+v", views)
	}

	e.retag(t, n, "iam", "s3")
	revived := read()
	if revived.deleted || !revived.createdAt.Equal(before.createdAt) {
		t.Fatalf("after re-add: %+v", revived)
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM note_tags WHERE note_id = $1`, n.ID).Scan(&rows)
	if rows != 2 {
		t.Fatalf("note_tags rows = %d, want 2 (no duplicate row)", rows)
	}
	if views, _ := e.repo.ListByTag(ctx, "iam", domain.SortSpec{}); len(views) != 1 {
		t.Fatalf("iam view = %+v", views)
	}
}

func TestNoteRepoTagViewAcrossSubjects(t *testing.T) { // AC-B22
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	e.tagged(t, "A", "iam")
	other := e.subj.add(t, e.subject.CategoryID, "Azure")
	b := e.note(t, other.ID, "B", "body")
	b.Tags = domain.RehydrateTagSet([]string{"iam"})
	if err := e.repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	views, err := e.repo.ListByTag(ctx, "iam", domain.SortSpec{})
	if err != nil || len(views) != 2 {
		t.Fatalf("views = (%+v, %v)", views, err)
	}
	for _, v := range views {
		if v.Subject == nil || v.Category == nil || v.Category.Path != "TI" || v.Category.Color != "#22D3EE" || v.Content.String() != "" {
			t.Errorf("bad view: %+v", v)
		}
	}
	if views[0].Subject.Name != "AWS" || views[1].Subject.Name != "Azure" {
		t.Errorf("subjects = %s, %s", views[0].Subject.Name, views[1].Subject.Name)
	}
	one, _ := e.repo.ListBySubject(ctx, e.subject.ID, ptrStr("iam"), domain.SortSpec{})
	if len(one) != 1 || one[0].Subject != nil {
		t.Errorf("subject filter = %+v", one)
	}
}

func ptrStr(s string) *string { return &s }

func TestTagQueryCountsAliveLinksOfAliveNotes(t *testing.T) { // AC-B22, S06-3
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	q := NewTagQuery(pool)
	a := e.tagged(t, "A", "iam", "s3")
	b := e.tagged(t, "B", "iam")
	e.tagged(t, "C", "ebs")

	counts, err := q.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.TagCount{{Name: "ebs", Count: 1}, {Name: "iam", Count: 2}, {Name: "s3", Count: 1}}
	if !slices.Equal(counts, want) {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}

	b.Delete(e.now)
	if err := e.repo.Delete(ctx, b); err != nil {
		t.Fatal(err)
	}
	e.retag(t, a, "iam")
	counts, _ = q.List(ctx)
	want = []domain.TagCount{{Name: "ebs", Count: 1}, {Name: "iam", Count: 1}}
	if !slices.Equal(counts, want) {
		t.Fatalf("after delete/unlink = %+v, want %+v", counts, want)
	}
}

func TestConcurrentAttachOfTheSameNewTag(t *testing.T) { // S06-7
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	const writers = 6
	notes := make([]domain.Note, writers)
	for i := range notes {
		n := e.note(t, e.subject.ID, string(rune('A'+i)), "")
		n.Tags = domain.RehydrateTagSet([]string{"shared"})
		notes[i] = n
	}
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for _, n := range notes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- e.repo.Create(ctx, n)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM tags WHERE name = 'shared'`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("tags rows = %d, want 1", rows)
	}
	counts, _ := NewTagQuery(pool).List(ctx)
	if len(counts) != 1 || counts[0].Count != writers {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestSlabLinksRoundTripInOrder(t *testing.T) { // S06-9
	e := newNoteRepoEnv(t)
	ctx := context.Background()
	n := e.note(t, e.subject.ID, "B-Tree", "")
	links, err := domain.NewSlabLinks([]domain.SlabLinkInput{
		{Label: "Second", Path: "/slab/b"}, {Label: "First", Path: "/slab/a/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	n.SlabLinks = links
	if err := e.repo.Create(ctx, n); err != nil {
		t.Fatal(err)
	}
	v, _ := e.repo.GetView(ctx, n.ID)
	if !slices.Equal(v.SlabLinks, links) {
		t.Fatalf("links = %+v, want %+v", v.SlabLinks, links)
	}
	list, _ := e.repo.ListBySubject(ctx, e.subject.ID, nil, domain.SortSpec{})
	if len(list[0].SlabLinks) != 0 {
		t.Fatalf("summary carries slab links: %+v", list[0].SlabLinks)
	}
	// An edit that does not touch them keeps them.
	cur, _ := e.repo.Get(ctx, n.ID)
	cur.Content = domain.RehydrateMarkdownContent("x")
	cur.Touch(e.now.Add(time.Minute))
	if err := e.repo.Update(ctx, cur, 1); err != nil {
		t.Fatal(err)
	}
	if v, _ = e.repo.GetView(ctx, n.ID); !slices.Equal(v.SlabLinks, links) {
		t.Fatalf("links lost on edit: %+v", v.SlabLinks)
	}
}

func TestTagTablesRejectHardDelete(t *testing.T) { // S06-8
	e := newNoteRepoEnv(t)
	e.tagged(t, "A", "iam")
	for _, table := range []string{"note_tags", "tags"} {
		_, err := pool.Exec(context.Background(), `DELETE FROM `+table)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
			t.Errorf("%s: err = %v, want SQLSTATE 23001", table, err)
		}
	}
}
