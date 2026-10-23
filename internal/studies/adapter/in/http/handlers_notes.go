package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/platform/httpx"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

func handleListSubjectNotes(list in.ListSubjectNotes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		sort, err := domain.ParseSortSpec(q.Get("sort"), q.Get("order"))
		if err != nil {
			translateError(w, err)
			return
		}
		var tag *string
		if q.Has("tag") {
			value := q.Get("tag")
			tag = &value
		}
		views, err := list.Execute(r.Context(), id, tag, sort)
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]noteSummaryDTO, len(views))
		for i, v := range views {
			dtos[i] = toNoteSummaryDTO(v)
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleCreateNote(create in.CreateNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req createNoteRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		kindID, err := requiredUUIDField("kindId", req.KindID)
		if err != nil {
			translateError(w, err)
			return
		}
		view, err := create.Execute(r.Context(), in.CreateNoteCommand{
			SubjectID: subjectID, Title: req.Title, KindID: kindID, Content: req.Content,
			Tags: req.Tags, SlabLinks: toLinkInputs(req.SlabLinks),
		})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/notes/"+view.ID)
		httpx.WriteData(w, http.StatusCreated, toNoteDTO(view))
	}
}

func handleGetNote(get in.GetNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		view, err := get.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toNoteDTO(view))
	}
}

func handleUpdateNote(update in.UpdateNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req updateNoteRequest
		if !httpx.DecodeJSON(w, r, &req) {
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
			Tags: req.Tags.patch(), SlabLinks: slabLinksPatch(req.SlabLinks),
		})
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toNoteDTO(view))
	}
}

func handleDeleteNote(del in.DeleteNote) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
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

func slabLinksPatch(o Opt[[]slabLinkDTO]) in.Patch[[]domain.SlabLinkInput] {
	p := in.Patch[[]domain.SlabLinkInput]{Set: o.Set}
	if o.Value != nil {
		inputs := toLinkInputs(*o.Value)
		p.Value = &inputs
	}
	return p
}

func handleListNotesByTag(list in.ListNotesByTag) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sort, err := domain.ParseSortSpec(q.Get("sort"), q.Get("order"))
		if err != nil {
			translateError(w, err)
			return
		}
		views, err := list.Execute(r.Context(), q.Get("tag"), sort)
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]noteSummaryDTO, len(views))
		for i, v := range views {
			dtos[i] = toNoteSummaryDTO(v)
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleListTags(list in.ListTags) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counts, err := list.Execute(r.Context())
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]tagDTO, len(counts))
		for i, c := range counts {
			dtos[i] = tagDTO{Name: c.Name, Count: c.Count}
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}
