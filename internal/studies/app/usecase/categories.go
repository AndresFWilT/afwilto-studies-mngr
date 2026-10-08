package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/out"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// placementChain resolves the alive ancestor chain root → parent for a new
// placement. A nil parent means the root (empty chain). A missing or deleted
// parent is a 422 on parentId (BR-DEL2), not a 404 on the category itself.
func placementChain(ctx context.Context, repo out.CategoryRepository, parentID *string) ([]string, error) {
	if parentID == nil {
		return nil, nil
	}
	chain, err := repo.AncestorIDs(ctx, *parentID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.SingleViolation("parentId", "not_found", "parent category does not exist")
	}
	return chain, err
}

// ── Create ──────────────────────────────────────────────────────────────────

type createCategoryService struct {
	repo  out.CategoryRepository
	clock out.Clock
	ids   out.IDGenerator
}

func NewCreateCategoryService(repo out.CategoryRepository, clock out.Clock, ids out.IDGenerator) in.CreateCategory {
	return &createCategoryService{repo: repo, clock: clock, ids: ids}
}

func (s *createCategoryService) Execute(ctx context.Context, cmd in.CreateCategoryCommand) (domain.CategoryView, error) {
	name, err := domain.NewCategoryName(cmd.Name)
	if err != nil {
		return domain.CategoryView{}, err
	}
	color, err := domain.NewHexColor(cmd.Color)
	if err != nil {
		return domain.CategoryView{}, err
	}
	chain, err := placementChain(ctx, s.repo, cmd.ParentID)
	if err != nil {
		return domain.CategoryView{}, err
	}
	if err := domain.CheckPlacement("", chain, 1); err != nil {
		return domain.CategoryView{}, err
	}

	cat := domain.NewCategory(s.ids.NewID(), name, color, cmd.ParentID, s.clock.Now())
	if err := s.repo.Create(ctx, cat); err != nil {
		return domain.CategoryView{}, err
	}
	return s.repo.GetView(ctx, cat.ID)
}

// ── Get / List ──────────────────────────────────────────────────────────────

type getCategoryService struct{ repo out.CategoryRepository }

func NewGetCategoryService(repo out.CategoryRepository) in.GetCategory {
	return &getCategoryService{repo: repo}
}

func (s *getCategoryService) Execute(ctx context.Context, id string) (domain.CategoryView, error) {
	return s.repo.GetView(ctx, id)
}

type listCategoriesService struct{ repo out.CategoryRepository }

func NewListCategoriesService(repo out.CategoryRepository) in.ListCategories {
	return &listCategoriesService{repo: repo}
}

func (s *listCategoriesService) Execute(ctx context.Context) ([]domain.CategoryView, error) {
	return s.repo.List(ctx)
}

// ── Update ──────────────────────────────────────────────────────────────────

type updateCategoryService struct {
	repo  out.CategoryRepository
	clock out.Clock
}

func NewUpdateCategoryService(repo out.CategoryRepository, clock out.Clock) in.UpdateCategory {
	return &updateCategoryService{repo: repo, clock: clock}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func sameParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *updateCategoryService) Execute(ctx context.Context, id string, cmd in.UpdateCategoryCommand) (domain.CategoryView, error) {
	cat, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.CategoryView{}, err
	}

	if cmd.Name.Set {
		if cat.Name, err = domain.NewCategoryName(deref(cmd.Name.Value)); err != nil {
			return domain.CategoryView{}, err
		}
	}
	if cmd.Color.Set {
		if cat.Color, err = domain.NewHexColor(deref(cmd.Color.Value)); err != nil {
			return domain.CategoryView{}, err
		}
	}
	if cmd.ParentID.Set && !sameParent(cmd.ParentID.Value, cat.ParentID) {
		chain, err := placementChain(ctx, s.repo, cmd.ParentID.Value)
		if err != nil {
			return domain.CategoryView{}, err
		}
		height, err := s.repo.SubtreeHeight(ctx, id)
		if err != nil {
			return domain.CategoryView{}, err
		}
		if err := domain.CheckPlacement(id, chain, height); err != nil {
			return domain.CategoryView{}, err
		}
		cat.ParentID = cmd.ParentID.Value
	}

	cat.UpdatedAt = s.clock.Now()
	if err := s.repo.Update(ctx, cat); err != nil {
		return domain.CategoryView{}, err
	}
	return s.repo.GetView(ctx, id)
}

// ── Delete ──────────────────────────────────────────────────────────────────

type deleteCategoryService struct {
	repo  out.CategoryRepository
	clock out.Clock
}

func NewDeleteCategoryService(repo out.CategoryRepository, clock out.Clock) in.DeleteCategory {
	return &deleteCategoryService{repo: repo, clock: clock}
}

// Execute deletes logically. Alive subcategories block it (BR-DEL1); alive
// subjects join that rule in Slice 04, when the subjects table exists.
func (s *deleteCategoryService) Execute(ctx context.Context, id string) error {
	cat, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	children, err := s.repo.CountAliveChildren(ctx, id)
	if err != nil {
		return err
	}
	if children > 0 {
		return domain.NewConflict("id", domain.RuleInUse,
			fmt.Sprintf("category has %d subcategories; move or delete them first", children))
	}
	cat.Delete(s.clock.Now())
	return s.repo.Update(ctx, cat)
}
