package http

import (
	"net/http"
	"testing"
)

func TestTagViewAndCountsAPI(t *testing.T) { // S06-3, S06-6, AC-B22
	f := newNoteFixture(t)
	cat := f.createCategory(t, "Other", "")
	azure := f.createSubject(t, cat["id"].(string), "Azure")
	post := func(path, title, tags string) map[string]any {
		rec := f.do(t, http.MethodPost, path, f.body(title, `"tags":[`+tags+`]`))
		if rec.Code != 201 {
			t.Fatalf("create %s: %d %s", title, rec.Code, rec.Body.String())
		}
		return decodeData(t, rec)
	}
	a := post(f.listPath, "A", `"iam","s3"`)
	b := post(subjectsPath+"/"+azure["id"].(string)+"/notes", "B", `"IAM"`)

	counts := decodeList(t, f.do(t, http.MethodGet, "/api/v1/tags", ""))
	if len(counts) != 2 || counts[0]["name"] != "iam" || counts[0]["count"] != float64(2) || counts[1]["name"] != "s3" || counts[1]["count"] != float64(1) {
		t.Fatalf("counts = %v", counts)
	}

	view := decodeList(t, f.do(t, http.MethodGet, notesPath+"?tag=%20IAM%20", ""))
	if len(view) != 2 {
		t.Fatalf("view = %v", view)
	}
	for _, item := range view {
		subj, cat := item["subject"].(map[string]any), item["category"].(map[string]any)
		if subj["name"] == nil || cat["path"] == nil || cat["color"] != "#22D3EE" {
			t.Errorf("missing subject/category: %v", item)
		}
		if _, has := item["content"]; has {
			t.Errorf("tag view has content: %v", item)
		}
	}
	if sub := decodeList(t, f.do(t, http.MethodGet, f.listPath+"?tag=IAM", "")); len(sub) != 1 || sub[0]["id"] != a["id"] {
		t.Errorf("subject filter = %v", sub)
	} else if _, has := sub[0]["subject"]; has {
		t.Errorf("subject listing must not carry subject/category: %v", sub[0])
	}

	// sorting works on the tag view
	desc := decodeList(t, f.do(t, http.MethodGet, notesPath+"?tag=iam&sort=title&order=desc", ""))
	if desc[0]["title"] != "B" {
		t.Errorf("desc = %v", desc)
	}

	// deleting a note and unlinking a tag drop them from counts and views
	f.do(t, http.MethodDelete, notesPath+"/"+b["id"].(string), "")
	f.do(t, http.MethodPatch, notesPath+"/"+a["id"].(string), `{"version":1,"tags":["s3"]}`)
	counts = decodeList(t, f.do(t, http.MethodGet, "/api/v1/tags", ""))
	if len(counts) != 1 || counts[0]["name"] != "s3" {
		t.Errorf("counts after = %v", counts)
	}
	if got := decodeList(t, f.do(t, http.MethodGet, notesPath+"?tag=iam", "")); len(got) != 0 {
		t.Errorf("iam view = %v", got)
	}
	// re-adding revives
	f.do(t, http.MethodPatch, notesPath+"/"+a["id"].(string), `{"version":2,"tags":["iam","s3"]}`)
	if got := decodeList(t, f.do(t, http.MethodGet, notesPath+"?tag=iam", "")); len(got) != 1 {
		t.Errorf("iam view after re-add = %v", got)
	}
}

func TestTagViewErrorsAPI(t *testing.T) { // S06-6
	f := newNoteFixture(t)
	wantError(t, f.do(t, http.MethodGet, notesPath, ""), 422, "validation_failed", "tag", "required")
	wantError(t, f.do(t, http.MethodGet, notesPath+"?tag=", ""), 422, "validation_failed", "tag", "required")
	wantError(t, f.do(t, http.MethodGet, notesPath+"?tag=%20%20", ""), 422, "validation_failed", "tag", "required")
	wantError(t, f.do(t, http.MethodGet, notesPath+"?tag=bad_tag!", ""), 422, "validation_failed", "tag", "format")
	wantError(t, f.do(t, http.MethodGet, notesPath+"?tag=iam&sort=size", ""), 422, "validation_failed", "sort", "enum")
	if got := decodeList(t, f.do(t, http.MethodGet, notesPath+"?tag=unknown", "")); len(got) != 0 {
		t.Errorf("unknown tag = %v", got)
	}
	if got := decodeList(t, f.do(t, http.MethodGet, "/api/v1/tags", "")); len(got) != 0 {
		t.Errorf("empty tags = %v", got)
	}
}
