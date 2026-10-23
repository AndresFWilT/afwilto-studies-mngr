package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/afwilto/studies-mngr/api"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// QuestionFileReader runs stages 1 (parse) and 2 (JSON Schema) of the import
// pipeline (slice 03 §1.3) and turns a valid document into a
// domain.QuestionFile. The schema is the embedded api/questions.schema.json,
// compiled once.
type QuestionFileReader struct {
	schema  *jsonschema.Schema
	printer *message.Printer
}

// NewQuestionFileReader compiles the embedded schema. The composition root
// calls it at startup and refuses to start when it fails.
func NewQuestionFileReader() (*QuestionFileReader, error) {
	raw, err := api.FS.ReadFile(api.SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("reading the embedded question schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing the embedded question schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(api.SchemaURL, doc); err != nil {
		return nil, fmt.Errorf("registering the question schema: %w", err)
	}
	schema, err := compiler.Compile(api.SchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compiling the question schema: %w", err)
	}
	return &QuestionFileReader{schema: schema, printer: message.NewPrinter(language.English)}, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Read parses data and validates it against the schema. It returns the parsed
// file, or every error of the first failing stage and no file.
func (r *QuestionFileReader) Read(data []byte) (*domain.QuestionFile, []domain.ImportError) {
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		return nil, []domain.ImportError{jsonError("the file is not valid UTF-8")}
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, []domain.ImportError{jsonError(describeParseError(err))}
	}
	if err := r.schema.Validate(instance); err != nil {
		var invalid *jsonschema.ValidationError
		if errors.As(err, &invalid) {
			return nil, r.schemaErrors(instance, invalid)
		}
		return nil, []domain.ImportError{{Path: "$", Rule: domain.RuleSchema, Message: err.Error()}}
	}

	var dto fileDTO
	if err := json.Unmarshal(data, &dto); err != nil { // the schema passed, so this is a bug, not bad input
		return nil, []domain.ImportError{{Path: "$", Rule: domain.RuleSchema, Message: "the file could not be read: " + err.Error()}}
	}
	return dto.toDomain(), nil
}

func jsonError(msg string) domain.ImportError {
	return domain.ImportError{Path: "$", Rule: domain.RuleJSON, Message: msg}
}

func describeParseError(err error) string {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "the file is empty or ends before the JSON document is complete"
	}
	return "the file is not valid JSON: " + err.Error()
}

// ── Stage 2: schema errors ──────────────────────────────────────────────────

// schemaErrors reports the leaves of the validator's error tree, each at a
// path like questions[3].options. An anyOf/oneOf node is one error, not one
// per branch.
func (r *QuestionFileReader) schemaErrors(instance any, root *jsonschema.ValidationError) []domain.ImportError {
	type located struct {
		tokens []string
		err    domain.ImportError
	}
	var found []located
	seen := map[string]bool{}
	add := func(tokens []string, msg string) {
		path := jsonPath(instance, tokens)
		if key := path + "\x00" + msg; !seen[key] {
			seen[key] = true
			found = append(found, located{tokens: tokens, err: domain.ImportError{Path: path, Rule: domain.RuleSchema, Message: msg}})
		}
	}

	var walk func(ve *jsonschema.ValidationError)
	walk = func(ve *jsonschema.ValidationError) {
		switch ve.ErrorKind.(type) {
		case *kind.AnyOf, *kind.OneOf:
			var parts []string
			for _, cause := range ve.Causes {
				parts = append(parts, r.leafMessages(cause)...)
			}
			add(ve.InstanceLocation, "does not match any allowed alternative: "+strings.Join(parts, "; "))
			return
		}
		switch ve.ErrorKind.(type) {
		case *kind.Contains, *kind.MinContains, *kind.MaxContains:
			// the causes are the per-item failures of the `contains` subschema;
			// the array itself is what is wrong
			add(ve.InstanceLocation, ve.ErrorKind.LocalizedString(r.printer))
			return
		}
		if len(ve.Causes) == 0 {
			add(ve.InstanceLocation, ve.ErrorKind.LocalizedString(r.printer))
			return
		}
		for _, cause := range ve.Causes {
			walk(cause)
		}
	}
	walk(root)

	sort.SliceStable(found, func(i, j int) bool { return compareTokens(found[i].tokens, found[j].tokens) < 0 })
	errs := make([]domain.ImportError, len(found))
	for i, f := range found {
		errs[i] = f.err
	}
	return errs
}

func (r *QuestionFileReader) leafMessages(ve *jsonschema.ValidationError) []string {
	if len(ve.Causes) == 0 {
		return []string{ve.ErrorKind.LocalizedString(r.printer)}
	}
	var out []string
	for _, cause := range ve.Causes {
		out = append(out, r.leafMessages(cause)...)
	}
	return out
}

// compareTokens orders JSON pointers document-wise: array indexes numerically.
func compareTokens(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		na, errA := strconv.Atoi(a[i])
		nb, errB := strconv.Atoi(b[i])
		if errA == nil && errB == nil {
			return na - nb
		}
		return strings.Compare(a[i], b[i])
	}
	return len(a) - len(b)
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// jsonPath renders a JSON pointer in the report's style: "$" for the document,
// questions[3].options for nested values, a bracketed string for keys that are
// not identifiers. The instance tells arrays (indexes) from objects (keys).
func jsonPath(instance any, tokens []string) string {
	if len(tokens) == 0 {
		return "$"
	}
	var b strings.Builder
	node := instance
	for i, tok := range tokens {
		switch parent := node.(type) {
		case []any:
			b.WriteString("[" + tok + "]")
			if n, err := strconv.Atoi(tok); err == nil && n >= 0 && n < len(parent) {
				node = parent[n]
			}
		case map[string]any:
			switch {
			case !identifier.MatchString(tok):
				quoted, _ := json.Marshal(tok)
				b.WriteString("[" + string(quoted) + "]")
			case i == 0:
				b.WriteString(tok)
			default:
				b.WriteString("." + tok)
			}
			node = parent[tok]
		default:
			b.WriteString("." + tok)
		}
	}
	return b.String()
}

// ── Decoding a valid document ───────────────────────────────────────────────

type fileDTO struct {
	Source    *sourceDTO        `json:"source"`
	Questions []fileQuestionDTO `json:"questions"`
}

type sourceDTO struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type optionDTO struct {
	Text      string `json:"text"`
	Image     string `json:"image"`
	Alt       string `json:"alt"`
	Correct   bool   `json:"correct"`
	Rationale string `json:"rationale"`
}

type originDTO struct {
	Set     string `json:"set"`
	Number  int    `json:"number"`
	NoteRef string `json:"noteRef"`
}

type fileQuestionDTO struct {
	Key         string            `json:"key"`
	Type        string            `json:"type"`
	Stem        string            `json:"stem"`
	Options     []optionDTO       `json:"options"`
	Explanation string            `json:"explanation"`
	Difficulty  string            `json:"difficulty"`
	Categories  orderedCategories `json:"categories"`
	Origin      *originDTO        `json:"origin"`
	Source      *sourceDTO        `json:"source"`
}

// orderedCategories reads the categories object keeping the key order of the
// document, which decides the order of first appearance (FR-3.1).
type orderedCategories []domain.ClassifiedNames

func (o *orderedCategories) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("categories must be an object")
	}
	var out []domain.ClassifiedNames
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		var names []string
		if err := dec.Decode(&names); err != nil {
			return err
		}
		out = append(out, domain.ClassifiedNames{Classification: keyTok.(string), Names: names})
	}
	*o = out
	return nil
}

func (s *sourceDTO) toDomain() *domain.Source {
	if s == nil {
		return nil
	}
	return &domain.Source{Kind: s.Kind, Name: s.Name}
}

func (dto fileDTO) toDomain() *domain.QuestionFile {
	file := &domain.QuestionFile{Source: dto.Source.toDomain(), Questions: make([]domain.FileQuestion, len(dto.Questions))}
	for i, q := range dto.Questions {
		fq := domain.FileQuestion{
			Key: q.Key, Type: domain.QuestionType(q.Type), Stem: q.Stem,
			Explanation: q.Explanation, Difficulty: q.Difficulty,
			Categories: slices.Clone(q.Categories), Source: q.Source.toDomain(),
		}
		for _, o := range q.Options {
			fq.Options = append(fq.Options, domain.Option{Text: o.Text, Image: o.Image, Alt: o.Alt, Correct: o.Correct, Rationale: o.Rationale})
		}
		if q.Origin != nil {
			fq.Origin = &domain.Origin{Set: q.Origin.Set, Number: q.Origin.Number, NoteRef: q.Origin.NoteRef}
		}
		file.Questions[i] = fq
	}
	return file
}
