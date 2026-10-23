package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/platform/httpx"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

func handleListCategories(list in.ListCategories) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		views, err := list.Execute(r.Context())
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]categoryDTO, len(views))
		for i, v := range views {
			dtos[i] = toCategoryDTO(v)
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleCreateCategory(create in.CreateCategory) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createCategoryRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		parentID, err := uuidField("parentId", req.ParentID)
		if err != nil {
			translateError(w, err)
			return
		}
		view, err := create.Execute(r.Context(), in.CreateCategoryCommand{
			Name: req.Name, Color: req.Color, ParentID: parentID,
		})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/categories/"+view.ID)
		httpx.WriteData(w, http.StatusCreated, toCategoryDTO(view))
	}
}

func handleGetCategory(get in.GetCategory) http.HandlerFunc {
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
		httpx.WriteData(w, http.StatusOK, toCategoryDTO(view))
	}
}

func handleUpdateCategory(update in.UpdateCategory) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req updateCategoryRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		if !req.Name.Set && !req.Color.Set && !req.ParentID.Set {
			translateError(w, domain.SingleViolation("body", "required", "at least one of name, color or parentId must be provided"))
			return
		}
		var err error
		if req.ParentID.Value, err = uuidField("parentId", req.ParentID.Value); err != nil {
			translateError(w, err)
			return
		}
		view, err := update.Execute(r.Context(), id, in.UpdateCategoryCommand{
			Name: req.Name.patch(), Color: req.Color.patch(), ParentID: req.ParentID.patch(),
		})
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toCategoryDTO(view))
	}
}

func handleDeleteCategory(del in.DeleteCategory) http.HandlerFunc {
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
