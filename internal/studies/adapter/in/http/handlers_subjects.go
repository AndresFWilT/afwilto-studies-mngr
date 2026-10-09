package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// requiredUUIDField canonicalizes a mandatory id field of a request body.
func requiredUUIDField(field string, value *string) (string, error) {
	if value == nil {
		return "", domain.SingleViolation(field, "required", field+" is required")
	}
	canonical, err := uuidField(field, value)
	if err != nil {
		return "", err
	}
	return *canonical, nil
}

func handleListSubjects(list in.ListSubjects) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var filter *string
		if raw, ok := r.URL.Query()["categoryId"]; ok {
			value := raw[0]
			var err error
			if filter, err = uuidField("categoryId", &value); err != nil {
				translateError(w, err)
				return
			}
		}
		views, err := list.Execute(r.Context(), filter)
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]subjectDTO, len(views))
		for i, v := range views {
			dtos[i] = toSubjectDTO(v)
		}
		writeList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleCreateSubject(create in.CreateSubject) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createSubjectRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		categoryID, err := requiredUUIDField("categoryId", req.CategoryID)
		if err != nil {
			translateError(w, err)
			return
		}
		view, err := create.Execute(r.Context(), in.CreateSubjectCommand{
			CategoryID: categoryID, Name: req.Name, Description: req.Description,
		})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/subjects/"+view.ID)
		writeData(w, http.StatusCreated, toSubjectDTO(view))
	}
}

func handleGetSubject(get in.GetSubject) http.HandlerFunc {
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
		writeData(w, http.StatusOK, toSubjectDTO(view))
	}
}

func handleUpdateSubject(update in.UpdateSubject) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var req updateSubjectRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if !req.CategoryID.Set && !req.Name.Set && !req.Description.Set {
			translateError(w, domain.SingleViolation("body", "required", "at least one of categoryId, name or description must be provided"))
			return
		}
		var err error
		if req.CategoryID.Value, err = uuidField("categoryId", req.CategoryID.Value); err != nil {
			translateError(w, err)
			return
		}
		view, err := update.Execute(r.Context(), id, in.UpdateSubjectCommand{
			CategoryID: req.CategoryID.patch(), Name: req.Name.patch(), Description: req.Description.patch(),
		})
		if err != nil {
			translateError(w, err)
			return
		}
		writeData(w, http.StatusOK, toSubjectDTO(view))
	}
}

func handleDeleteSubject(del in.DeleteSubject) http.HandlerFunc {
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
