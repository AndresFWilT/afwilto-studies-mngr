package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// TagQuery reads the tags derived from note links.
type TagQuery interface {
	// List returns the tags with at least one alive link to an alive note,
	// with that count, ordered by name.
	List(ctx context.Context) ([]domain.TagCount, error)
}
