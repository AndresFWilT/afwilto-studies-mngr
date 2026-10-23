// Package api holds the canonical, embedded copies of the contract artifacts
// that are not OpenAPI: the question import JSON Schema and its examples
// (Spec 18 slice 03 §1.1). The backend compiles the schema once at startup and
// the offline tooling reads the same file.
package api

import "embed"

// SchemaPath is the schema's path inside FS.
const SchemaPath = "questions.schema.json"

// SchemaURL is the schema's $id, the URL the validator registers it under.
const SchemaURL = "https://afwilto.local/schemas/afwilto-questions.v1.json"

// FS holds questions.schema.json and examples/*.json.
//
//go:embed questions.schema.json examples/*.json
var FS embed.FS
