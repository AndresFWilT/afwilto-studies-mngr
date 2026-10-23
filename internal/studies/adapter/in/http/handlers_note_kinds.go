package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/platform/httpx"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
)

func handleListNoteKinds(list in.ListNoteKinds) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		views, err := list.Execute(r.Context())
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]noteKindDTO, len(views))
		for i, v := range views {
			dtos[i] = toNoteKindDTO(v)
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleCreateNoteKind(create in.CreateNoteKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req noteKindRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		view, err := create.Execute(r.Context(), in.CreateNoteKindCommand{Name: req.Name})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/note-kinds/"+view.ID)
		httpx.WriteData(w, http.StatusCreated, toNoteKindDTO(view))
	}
}

func handleGetNoteKind(get in.GetNoteKind) http.HandlerFunc {
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
		httpx.WriteData(w, http.StatusOK, toNoteKindDTO(view))
	}
}

func handleRenameNoteKind(rename in.RenameNoteKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req noteKindRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		view, err := rename.Execute(r.Context(), id, in.RenameNoteKindCommand{Name: req.Name})
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toNoteKindDTO(view))
	}
}

func handleDeleteNoteKind(del in.DeleteNoteKind) http.HandlerFunc {
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
