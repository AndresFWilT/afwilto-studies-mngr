package out

import "context"

// NoteDirectory finds the notes of an exam's subject. Notes belong to the
// studies context, which exams never imports (Spec 18 D-10); the adapter reads
// them through SQL.
type NoteDirectory interface {
	// FindNoteIDs maps each of the given titles to the id of the alive note of
	// the exam's subject with exactly that title. A title without a note is
	// absent from the map (a valid empty result, not an error).
	FindNoteIDs(ctx context.Context, examID string, titles []string) (map[string]string, error)
}
