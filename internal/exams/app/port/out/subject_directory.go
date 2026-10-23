package out

import "context"

// SubjectDirectory answers whether a subject exists. The subject belongs to
// the studies context, which exams never imports (Spec 18 D-10); the adapter
// reads it through SQL.
type SubjectDirectory interface {
	// IsAlive reports whether the subject exists and is not logically deleted.
	IsAlive(ctx context.Context, subjectID string) (bool, error)
}
