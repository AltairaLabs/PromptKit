// Package schema provides embedded PromptPack schema for offline validation.
//
// # promptpack.schema.json is a verbatim mirror — never edit it
//
// The embedded promptpack.schema.json is a byte-for-byte copy of the published
// PromptPack release at https://promptpack.org/schema/latest/. PromptKit does not
// own it and must never hand-edit it: not to add a field the runtime wants, not
// to delete one the runtime doesn't implement, not to tighten an enum the spec
// leaves open.
//
// The only sanctioned way to change it is scripts/fetch-promptpack-schema.sh
// (make promptpack-schema). CI fails on any difference (make
// promptpack-schema-check).
//
// Where the runtime deliberately declines to carry a spec property, record it as
// a deliberateOmission in runtime/prompt/validator_spec_parity_test.go, in Go,
// where a reviewer sees it. Editing the schema to make a parity test pass makes
// the guard circular — it grades promptkit's types against a document promptkit
// edits — and that is exactly how this file drifted 143 leaves from the spec
// between 2026-06 and 2026-08 while still claiming to be v1.5.0, silently
// rejecting packs that were valid against promptpack.org.
package schema

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed promptpack.schema.json
var embeddedSchema string

// DefaultSchemaURL is the canonical URL for the PromptPack schema.
const DefaultSchemaURL = "https://promptpack.org/schema/latest/promptpack.schema.json"

// SchemaSourceEnvVar is the environment variable to override schema source.
// Values: "local" (embedded), "remote" (fetch from URL), or a file path.
const SchemaSourceEnvVar = "PROMPTKIT_SCHEMA_SOURCE"

// GetSchemaLoader returns a gojsonschema loader for the PromptPack schema.
// Priority:
//  1. If PROMPTKIT_SCHEMA_SOURCE is set to "local", use embedded schema
//  2. If PROMPTKIT_SCHEMA_SOURCE is a file path, load from that file
//  3. If PROMPTKIT_SCHEMA_SOURCE is "remote" and packSchemaURL is provided, fetch from that URL
//  4. Otherwise, use embedded schema (default for offline support)
func GetSchemaLoader(packSchemaURL string) (gojsonschema.JSONLoader, error) {
	source := os.Getenv(SchemaSourceEnvVar)

	switch {
	case source == "local" || source == "":
		// Use embedded schema (default for offline support)
		return gojsonschema.NewStringLoader(embeddedSchema), nil

	case source == "remote":
		// Explicitly request remote fetch
		url := packSchemaURL
		if url == "" {
			url = DefaultSchemaURL
		}
		return gojsonschema.NewReferenceLoader(url), nil

	case strings.HasPrefix(source, "/") || strings.HasPrefix(source, "./"):
		// File path provided
		data, err := os.ReadFile(source) //nolint:gosec // source is from trusted env var, not user input
		if err != nil {
			return nil, fmt.Errorf("failed to read schema from %s: %w", source, err)
		}
		return gojsonschema.NewStringLoader(string(data)), nil

	default:
		// Treat as URL
		return gojsonschema.NewReferenceLoader(source), nil
	}
}

// GetEmbeddedSchema returns the embedded schema as a string.
func GetEmbeddedSchema() string {
	return embeddedSchema
}

// GetEmbeddedSchemaVersion returns the version from the embedded schema.
func GetEmbeddedSchemaVersion() (string, error) {
	var schema struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(embeddedSchema), &schema); err != nil {
		return "", fmt.Errorf("failed to parse embedded schema: %w", err)
	}
	return schema.Version, nil
}

// ExtractSchemaURL extracts the $schema URL from pack JSON data.
// Returns empty string if not present or invalid.
func ExtractSchemaURL(packJSON []byte) string {
	var pack struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(packJSON, &pack); err != nil {
		return ""
	}
	return pack.Schema
}
