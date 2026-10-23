package out

import "context"

// SubjectExamCounter counts the alive exams of a subject, so deleting a
// subject can be blocked while exams hang off it (Spec 18 BR-E3). Exams belong
// to another bounded context; the adapter reads them through SQL only.
type SubjectExamCounter interface {
	CountAliveExams(ctx context.Context, subjectID string) (int, error)
}
