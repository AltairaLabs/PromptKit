package generators

import (
	"github.com/invopop/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// GenerateMetadataSchema generates the JSON Schema for Kubernetes ObjectMeta
func GenerateMetadataSchema() (interface{}, error) {
	reflector := newReflector("json")

	schema := reflector.Reflect(&metav1.ObjectMeta{})

	schema.ID = jsonschema.ID(schemaBaseURL + "/common/metadata.json")
	schema.Title = "Kubernetes ObjectMeta"
	schema.Description = "Kubernetes-style metadata for PromptKit resources"

	return schema, nil
}

// GenerateAssertionsSchema generates the standalone schema for a single
// PromptArena assertion (config.AssertionConfig). The `type` field is an open
// enum: PromptKit-known handler types are suggested, but any string is accepted
// so a different runtime's types are not rejected.
func GenerateAssertionsSchema() (interface{}, error) {
	reflector := newReflector("yaml")
	schema := reflector.Reflect(&config.AssertionConfig{})

	schema.Version = draftSchemaVersion
	schema.ID = jsonschema.ID(schemaBaseURL + "/common/assertions.json")
	schema.Title = "PromptArena Assertion"
	schema.Description = "A single assertion (type + optional params) for PromptArena scenarios"

	// ExpandedStruct puts AssertionConfig's fields at the top level, so apply
	// the open type enum directly to the root schema.
	applyOpenTypeEnum(schema, "type", knownEvalTypes(), typeEnumDescription)

	return schema, nil
}

// GenerateMediaSchema generates a placeholder for media schema
func GenerateMediaSchema() (interface{}, error) {
	schema := &jsonschema.Schema{
		ID:          jsonschema.ID(schemaBaseURL + "/common/media.json"),
		Title:       "PromptKit Media Types",
		Description: "Media content types for multimodal scenarios",
		Type:        "object",
	}

	return schema, nil
}

// allowSchemaField adds the $schema property to the schema's allowed properties.
// This is a standard JSON Schema field that users can use to reference the schema URL.
func allowSchemaField(schema *jsonschema.Schema) {
	if schema.Properties == nil {
		return
	}

	// Add $schema as an optional string property
	schemaProperty := &jsonschema.Schema{
		Type:        "string",
		Format:      "uri",
		Description: "JSON Schema reference URL",
	}

	schema.Properties.Set("$schema", schemaProperty)
}
