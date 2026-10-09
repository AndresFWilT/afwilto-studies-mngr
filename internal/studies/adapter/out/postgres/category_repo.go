package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

const uniqueViolation = "23505"

// recursionGuardSQL bounds every recursive walk well above the 5-level limit, so
// even a tree corrupted behind the trigger's back cannot make a query loop.
const recursionGuardSQL = "10"

type CategoryRepository struct {
	pool *pgxpool.Pool
}

func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

// categoryWriteError maps the unique index and the tree-guard trigger
// (migration 0003) to domain errors.
func categoryWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if pgErr.Code == uniqueViolation {
		return domain.NewConflict("name", domain.RuleDuplicateName, "a sibling category with this name already exists")
	}
	switch pgErr.ConstraintName {
	case "category_parent_not_found":
		return domain.SingleViolation("parentId", "not_found", "parent category does not exist")
	case "category_cycle":
		return domain.SingleViolation("parentId", "cycle", "a category cannot be placed under itself or one of its descendants")
	case "category_max_depth":
		return domain.SingleViolation("parentId", "max_depth", "the category tree is limited to 5 levels")
	case "category_in_use":
		return domain.NewConflict("id", domain.RuleInUse, "category still has subcategories or subjects; move or delete them first")
	}
	return err
}

func (r *CategoryRepository) Create(ctx context.Context, cat domain.Category) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO categories (id, parent_id, name, color, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		cat.ID, cat.ParentID, cat.Name.String(), cat.Color.String(), cat.CreatedAt, cat.UpdatedAt)
	return categoryWriteError(err)
}

func (r *CategoryRepository) Update(ctx context.Context, cat domain.Category) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE categories
		SET parent_id = $2, name = $3, color = $4, updated_at = $5, deleted_at = $6
		WHERE id = $1 AND deleted_at IS NULL`,
		cat.ID, cat.ParentID, cat.Name.String(), cat.Color.String(), cat.UpdatedAt, cat.DeletedAt)
	if err != nil {
		return categoryWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *CategoryRepository) Get(ctx context.Context, id string) (domain.Category, error) {
	var (
		cat         domain.Category
		name, color string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, parent_id, name, color, created_at, updated_at
		FROM categories WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&cat.ID, &cat.ParentID, &name, &color, &cat.CreatedAt, &cat.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Category{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Category{}, err
	}
	cat.Name = domain.RehydrateCategoryName(name)
	cat.Color = domain.RehydrateHexColor(color)
	return cat, nil
}

// aliveTree walks the alive tree from the roots down, deriving the display
// path, depth and a sort key. The key joins lower-cased names with \x01 —
// lower than any printable byte — so ordering it under COLLATE "C" compares
// path segment by segment: parents first, siblings alphabetical.
const aliveTree = `
	WITH RECURSIVE tree AS (
		SELECT id, parent_id, name, color, created_at, updated_at,
		       name::text AS path, lower(name)::text AS sort_key, 1 AS depth
		FROM categories
		WHERE parent_id IS NULL AND deleted_at IS NULL
		UNION ALL
		SELECT c.id, c.parent_id, c.name, c.color, c.created_at, c.updated_at,
		       t.path || ' / ' || c.name, t.sort_key || chr(1) || lower(c.name), t.depth + 1
		FROM categories c
		JOIN tree t ON c.parent_id = t.id
		WHERE c.deleted_at IS NULL AND t.depth < ` + recursionGuardSQL + `
	)
	SELECT id, parent_id, name, color, created_at, updated_at, path, depth,
	       (SELECT count(*) FROM subjects s WHERE s.category_id = tree.id AND s.deleted_at IS NULL)::int AS subject_count
	FROM tree`

func scanView(row pgx.Row) (domain.CategoryView, error) {
	var (
		v           domain.CategoryView
		name, color string
	)
	if err := row.Scan(&v.ID, &v.ParentID, &name, &color, &v.CreatedAt, &v.UpdatedAt,
		&v.Path, &v.Depth, &v.SubjectCount); err != nil {
		return domain.CategoryView{}, err
	}
	v.Name = domain.RehydrateCategoryName(name)
	v.Color = domain.RehydrateHexColor(color)
	return v, nil
}

func (r *CategoryRepository) GetView(ctx context.Context, id string) (domain.CategoryView, error) {
	v, err := scanView(r.pool.QueryRow(ctx, aliveTree+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CategoryView{}, domain.ErrNotFound
	}
	return v, err
}

func (r *CategoryRepository) List(ctx context.Context) ([]domain.CategoryView, error) {
	rows, err := r.pool.Query(ctx, aliveTree+` ORDER BY sort_key COLLATE "C", id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := []domain.CategoryView{}
	for rows.Next() {
		v, err := scanView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, rows.Err()
}

// AncestorIDs returns root → id. A chain that does not reach an alive root
// (unknown or deleted id) is ErrNotFound.
func (r *CategoryRepository) AncestorIDs(ctx context.Context, id string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id, 1 AS lvl
			FROM categories WHERE id = $1 AND deleted_at IS NULL
			UNION ALL
			SELECT c.id, c.parent_id, up.lvl + 1
			FROM categories c JOIN up ON c.id = up.parent_id
			WHERE c.deleted_at IS NULL AND up.lvl < `+recursionGuardSQL+`
		)
		SELECT id, parent_id IS NULL FROM up ORDER BY lvl DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		ids     []string
		reaches bool
	)
	for rows.Next() {
		var (
			ancestor string
			isRoot   bool
		)
		if err := rows.Scan(&ancestor, &isRoot); err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			reaches = isRoot
		}
		ids = append(ids, ancestor)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || !reaches {
		return nil, domain.ErrNotFound
	}
	return ids, nil
}

func (r *CategoryRepository) SubtreeHeight(ctx context.Context, id string) (int, error) {
	var height *int
	err := r.pool.QueryRow(ctx, `
		WITH RECURSIVE down AS (
			SELECT id, 1 AS lvl
			FROM categories WHERE id = $1 AND deleted_at IS NULL
			UNION ALL
			SELECT c.id, down.lvl + 1
			FROM categories c JOIN down ON c.parent_id = down.id
			WHERE c.deleted_at IS NULL AND down.lvl < `+recursionGuardSQL+`
		)
		SELECT max(lvl) FROM down`, id).Scan(&height)
	if err != nil {
		return 0, err
	}
	if height == nil {
		return 0, domain.ErrNotFound
	}
	return *height, nil
}

func (r *CategoryRepository) CountAliveChildren(ctx context.Context, id string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM categories WHERE parent_id = $1 AND deleted_at IS NULL`, id).Scan(&n)
	return n, err
}
