package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// notImplemented rejects input that belongs to Slice 06 (tags, slab links)
// instead of silently dropping it.
func notImplemented(field string) error {
	return domain.SingleViolation(field, "not_implemented", field+" are not supported yet")
}

func rejectSlice06Fields(tags []string, slabLinks []any) error {
	if len(tags) > 0 {
		return notImplemented("tags")
	}
	if len(slabLinks) > 0 {
		return notImplemented("slabLinks")
	}
	return nil
}

func handleListSubjectNotes(list in.ListSubjectNotes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		if q.Has("tag") {
			translateError(w, domain.SingleViolation("tag", "not_implemented", "the tag filter is not supported yet"))
			return
		}
		sort, err := domain.ParseSortSpec(q.Get("sort"), q.Get("order"))
		if err != nil {
			translateError(w, err)
			return
		}
		views, err := list.Execute(r.Context(), id, sort)
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]noteSummaryDTO, len(views))
		for i, v := range views {
			dtos[i] = toNoteSummaryDTO(v)
		}
		writeList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleCreateNote(create in.CreateNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, ok := pathID(w, r)
		if !ok {
			return
		}
		var req createNoteRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := rejectSlice06Fields(req.Tags, rawToAny(req.SlabLinks)); err != nil {
			translateError(w, err)
			return
		}
		kindID, err := requiredUUIDField("kindId", req.KindID)
		if err != nil {
			translateError(w, err)
			return
		}
		view, err := create.Execute(r.Context(), in.CreateNoteCommand{
			SubjectID: subjectID, Title: req.Title, KindID: kindID, Content: req.Content,
		})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/notes/"+view.ID)
		writeData(w, http.StatusCreated, toNoteDTO(view))
	}
}

func handleGetNote(get in.GetNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		view, err := get.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		writeData(w, http.StatusOK, toNoteDTO(view))
	}
}

func handleUpdateNote(update in.UpdateNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var req updateNoteRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Version == nil {
			translateError(w, domain.SingleViolation("version", "required", "version is required"))
			return
		}
		if *req.Version < 1 {
			translateError(w, domain.SingleViolation("version", "min", "version must be at least 1"))
			return
		}
		if err := rejectSlice06Fields(req.Tags, rawToAny(req.SlabLinks)); err != nil {
			translateError(w, err)
			return
		}
		var err error
		if req.KindID.Value, err = uuidField("kindId", req.KindID.Value); err != nil {
			translateError(w, err)
			return
		}
		if req.SubjectID.Value, err = uuidField("subjectId", req.SubjectID.Value); err != nil {
			translateError(w, err)
			return
		}
		view, err := update.Execute(r.Context(), id, in.UpdateNoteCommand{
			Version: *req.Version,
			Title:   req.Title.patch(), KindID: req.KindID.patch(),
			SubjectID: req.SubjectID.patch(), Content: req.Content.patch(),
		})
		if err != nil {
			translateError(w, err)
			return
		}
		writeData(w, http.StatusOK, toNoteDTO(view))
	}
}

func handleDeleteNote(del in.DeleteNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if err := del.Execute(r.Context(), id); err != nil {
			translateError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func rawToAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
