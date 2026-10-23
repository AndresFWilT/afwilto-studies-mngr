package http

import (
	"bytes"
	"encoding/json"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// The FR-2 file written by the export. Field order is the order of the
// example files; empty optional values are left out, as the schema requires.

type exportSource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type exportOption struct {
	Text      string `json:"text,omitempty"`
	Image     string `json:"image,omitempty"`
	Alt       string `json:"alt,omitempty"`
	Correct   bool   `json:"correct"`
	Rationale string `json:"rationale,omitempty"`
}

type exportOrigin struct {
	Set     string `json:"set,omitempty"`
	Number  int    `json:"number,omitempty"`
	NoteRef string `json:"noteRef,omitempty"`
}

type exportQuestion struct {
	Key         string            `json:"key"`
	Type        string            `json:"type"`
	Stem        string            `json:"stem"`
	Options     []exportOption    `json:"options"`
	Explanation string            `json:"explanation,omitempty"`
	Difficulty  string            `json:"difficulty,omitempty"`
	Categories  *exportCategories `json:"categories,omitempty"`
	Origin      *exportOrigin     `json:"origin,omitempty"`
	Source      *exportSource     `json:"source,omitempty"`
}

type exportFile struct {
	Format        string           `json:"format"`
	FormatVersion int              `json:"formatVersion"`
	Source        *exportSource    `json:"source,omitempty"`
	Questions     []exportQuestion `json:"questions"`
}

// exportCategories is the `categories` object: classification name to category
// names, keeping the order in which the classifications first appear.
type exportCategories struct {
	names []domain.ClassifiedNames
}

func (c exportCategories) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, cn := range c.names {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := marshalNoEscape(cn.Classification)
		if err != nil {
			return nil, err
		}
		names, err := marshalNoEscape(cn.Names)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(names)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// fileSource is the most common source of the bank, the first one met winning
// a tie; every question whose source differs carries its own (FR-5).
func fileSource(qs []domain.Question) (domain.Source, bool) {
	var order []domain.Source
	count := map[domain.Source]int{}
	for _, q := range qs {
		if count[q.Source] == 0 {
			order = append(order, q.Source)
		}
		count[q.Source]++
	}
	var best domain.Source
	for i, s := range order {
		if i == 0 || count[s] > count[best] {
			best = s
		}
	}
	return best, len(order) > 0
}

func groupCategories(refs []domain.CategoryRef) *exportCategories {
	if len(refs) == 0 {
		return nil
	}
	c := &exportCategories{}
	for _, ref := range refs {
		idx := -1
		for i := range c.names {
			if c.names[i].Classification == ref.Classification {
				idx = i
			}
		}
		if idx < 0 {
			c.names = append(c.names, domain.ClassifiedNames{Classification: ref.Classification})
			idx = len(c.names) - 1
		}
		c.names[idx].Names = append(c.names[idx].Names, ref.Category)
	}
	return c
}

// exportDocument renders the questions.json of an export. The questions must
// already carry export-form media references.
func exportDocument(qs []domain.Question) ([]byte, error) {
	file := exportFile{Format: "afwilto-questions", FormatVersion: 1, Questions: make([]exportQuestion, 0, len(qs))}
	common, any := fileSource(qs)
	if any {
		file.Source = &exportSource{Kind: common.Kind, Name: common.Name}
	}
	for _, q := range qs {
		eq := exportQuestion{
			Key: q.Key, Type: string(q.Type), Stem: q.Stem, Explanation: q.Explanation, Difficulty: q.Difficulty,
			Categories: groupCategories(q.Categories),
		}
		for _, o := range q.Options {
			eq.Options = append(eq.Options, exportOption{Text: o.Text, Image: o.Image, Alt: o.Alt, Correct: o.Correct, Rationale: o.Rationale})
		}
		if q.Origin != nil && *q.Origin != (domain.Origin{}) {
			eq.Origin = &exportOrigin{Set: q.Origin.Set, Number: q.Origin.Number, NoteRef: q.Origin.NoteRef}
		}
		if q.Source != common {
			eq.Source = &exportSource{Kind: q.Source.Kind, Name: q.Source.Name}
		}
		file.Questions = append(file.Questions, eq)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
