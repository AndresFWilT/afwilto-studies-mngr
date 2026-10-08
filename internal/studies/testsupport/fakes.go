// Package testsupport provides hand-written fakes of the out-ports (never
// mocks of concrete classes) plus deterministic clock/id fixtures, shared by
// use-case and API tests. Fakes follow the same visibility and uniqueness
// rules as the SQL adapters.
package testsupport

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

var FixedNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// StepClock starts at FixedNow and advances one minute per call, so ordering
// by timestamps is observable in tests.
type StepClock struct{ N int }

func (c *StepClock) Now() time.Time {
	c.N++
	return FixedNow.Add(time.Duration(c.N) * time.Minute)
}

// SeqIDGen yields valid, ordered UUIDs that contain hex letters, so tests
// exercise case handling the way real UUIDs would.
type SeqIDGen struct{ N int }

func (g *SeqIDGen) NewID() string {
	g.N++
	return fmt.Sprintf("0abcdef0-0000-7000-8000-%012d", g.N)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// canon mirrors Postgres' uuid cast: case-insensitive, and a malformed id is
// an error (a 500 in production), never a quiet "not found".
func canon(id string) (string, error) {
	if !uuidPattern.MatchString(id) {
		return "", fmt.Errorf("invalid input syntax for type uuid: %q", id)
	}
	return strings.ToLower(id), nil
}

func canonPtr(id *string) (*string, error) {
	if id == nil {
		return nil, nil
	}
	c, err := canon(*id)
	return &c, err
}

func canonCategory(cat domain.Category) (domain.Category, error) {
	var err error
	if cat.ID, err = canon(cat.ID); err != nil {
		return cat, err
	}
	cat.ParentID, err = canonPtr(cat.ParentID)
	return cat, err
}

// FakeCategoryRepo implements out.CategoryRepository in memory.
type FakeCategoryRepo struct {
	Rows map[string]domain.Category
}

func NewFakeCategoryRepo() *FakeCategoryRepo {
	return &FakeCategoryRepo{Rows: map[string]domain.Category{}}
}

func sameParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (r *FakeCategoryRepo) siblingClash(cat domain.Category) bool {
	for id, other := range r.Rows {
		if id != cat.ID && other.DeletedAt == nil && cat.DeletedAt == nil &&
			sameParent(other.ParentID, cat.ParentID) && other.Name.Key() == cat.Name.Key() {
			return true
		}
	}
	return false
}

func duplicateName() error {
	return domain.NewConflict("name", domain.RuleDuplicateName, "a sibling category with this name already exists")
}

func (r *FakeCategoryRepo) Create(_ context.Context, cat domain.Category) error {
	cat, err := canonCategory(cat)
	if err != nil {
		return err
	}
	if r.siblingClash(cat) {
		return duplicateName()
	}
	r.Rows[cat.ID] = cat
	return nil
}

func (r *FakeCategoryRepo) Update(_ context.Context, cat domain.Category) error {
	cat, err := canonCategory(cat)
	if err != nil {
		return err
	}
	current, ok := r.Rows[cat.ID]
	if !ok || current.DeletedAt != nil {
		return domain.ErrNotFound
	}
	if r.siblingClash(cat) {
		return duplicateName()
	}
	r.Rows[cat.ID] = cat
	return nil
}

func (r *FakeCategoryRepo) Get(_ context.Context, id string) (domain.Category, error) {
	id, err := canon(id)
	if err != nil {
		return domain.Category{}, err
	}
	cat, ok := r.Rows[id]
	if !ok || cat.DeletedAt != nil {
		return domain.Category{}, domain.ErrNotFound
	}
	return cat, nil
}

// chain walks up to the root; ok=false when any link is missing or deleted.
func (r *FakeCategoryRepo) chain(id string) ([]domain.Category, bool) {
	var up []domain.Category
	for cur := &id; cur != nil; {
		cat, ok := r.Rows[*cur]
		if !ok || cat.DeletedAt != nil {
			return nil, false
		}
		up = append(up, cat)
		cur = cat.ParentID
	}
	slices.Reverse(up)
	return up, true
}

func (r *FakeCategoryRepo) view(id string) (domain.CategoryView, []string, bool) {
	chain, ok := r.chain(id)
	if !ok {
		return domain.CategoryView{}, nil, false
	}
	names := make([]string, len(chain))
	keys := make([]string, len(chain))
	for i, c := range chain {
		names[i] = c.Name.String()
		keys[i] = c.Name.Key()
	}
	return domain.CategoryView{
		Category: chain[len(chain)-1],
		Path:     strings.Join(names, " / "),
		Depth:    len(chain),
	}, keys, true
}

func (r *FakeCategoryRepo) GetView(_ context.Context, id string) (domain.CategoryView, error) {
	id, err := canon(id)
	if err != nil {
		return domain.CategoryView{}, err
	}
	v, _, ok := r.view(id)
	if !ok {
		return domain.CategoryView{}, domain.ErrNotFound
	}
	return v, nil
}

func (r *FakeCategoryRepo) List(_ context.Context) ([]domain.CategoryView, error) {
	type entry struct {
		view domain.CategoryView
		keys []string
	}
	var entries []entry
	for id := range r.Rows {
		if v, keys, ok := r.view(id); ok {
			entries = append(entries, entry{v, keys})
		}
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if c := slices.Compare(a.keys, b.keys); c != 0 {
			return c
		}
		return strings.Compare(a.view.ID, b.view.ID)
	})
	out := make([]domain.CategoryView, len(entries))
	for i, e := range entries {
		out[i] = e.view
	}
	return out, nil
}

func (r *FakeCategoryRepo) AncestorIDs(_ context.Context, id string) ([]string, error) {
	id, err := canon(id)
	if err != nil {
		return nil, err
	}
	chain, ok := r.chain(id)
	if !ok {
		return nil, domain.ErrNotFound
	}
	ids := make([]string, len(chain))
	for i, c := range chain {
		ids[i] = c.ID
	}
	return ids, nil
}

func (r *FakeCategoryRepo) SubtreeHeight(ctx context.Context, id string) (int, error) {
	id, err := canon(id)
	if err != nil {
		return 0, err
	}
	if _, err := r.Get(ctx, id); err != nil {
		return 0, err
	}
	height := 1
	for childID, child := range r.Rows {
		if child.DeletedAt == nil && child.ParentID != nil && *child.ParentID == id {
			h, err := r.SubtreeHeight(ctx, childID)
			if err != nil {
				return 0, err
			}
			height = max(height, h+1)
		}
	}
	return height, nil
}

func (r *FakeCategoryRepo) CountAliveChildren(_ context.Context, id string) (int, error) {
	id, err := canon(id)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, child := range r.Rows {
		if child.DeletedAt == nil && child.ParentID != nil && *child.ParentID == id {
			n++
		}
	}
	return n, nil
}
