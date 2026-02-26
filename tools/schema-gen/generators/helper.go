package generators

import (
	"github.com/invopop/jsonschema"
)

// SchemaConfig holds the configuration for generating a JSON schema from a Go type.
type SchemaConfig struct {
	// Target is the Go struct instance to reflect (e.g., &config.ArenaConfig{}).
	Target interface{}
	// Filename is the schema output filename (e.g., "arena.json").
	Filename string
	// Title is the human-readable schema title.
	Title string
	// Description is the schema description.
	Description string
	// FieldNameTag overrides the struct tag used for field names.
	// Defaults to "yaml" if empty.
	FieldNameTag string
	// Customize is an optional callback to apply additional modifications
	// to the generated schema (e.g., adding examples or oneOf constraints).
	Customize func(*jsonschema.Schema)
}

// newReflector creates a jsonschema.Reflector with the standard configuration
// used across all schema generators.
func newReflector(fieldNameTag string) jsonschema.Reflector {
	if fieldNameTag == "" {
		fieldNameTag = "yaml"
	}
	return jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		ExpandedStruct:             true,
		FieldNameTag:               fieldNameTag,
		RequiredFromJSONSchemaTags: false,
	}
}

// Generate produces a JSON schema for the given SchemaConfig. It creates
// a reflector with standard settings, reflects the target type, sets the
// schema metadata (version, ID, title, description), adds the $schema
// property, and applies any custom modifications.
func Generate(cfg *SchemaConfig) (interface{}, error) {
	reflector := newReflector(cfg.FieldNameTag)

	schema := reflector.Reflect(cfg.Target)

	schema.Version = "https://json-schema.org/draft-07/schema"
	schema.ID = jsonschema.ID(schemaBaseURL + "/" + cfg.Filename)
	schema.Title = cfg.Title
	schema.Description = cfg.Description

	allowSchemaField(schema)

	if cfg.Customize != nil {
		cfg.Customize(schema)
	}

	return schema, nil
}
