package generators

import (
	"github.com/invopop/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateMetadataSchema generates the JSON Schema for Kubernetes ObjectMeta
func GenerateMetadataSchema() (interface{}, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		ExpandedStruct:            true,
	}

	schema := reflector.Reflect(&metav1.ObjectMeta{})

	schema.ID = schemaBaseURL + "/common/metadata.json"
	schema.Title = "Kubernetes ObjectMeta"
	schema.Description = "Kubernetes-style metadata for PromptKit resources"

	return schema, nil
}

// GenerateAssertionsSchema generates a placeholder for assertions schema
func GenerateAssertionsSchema() (interface{}, error) {
	schema := &jsonschema.Schema{
		ID:          schemaBaseURL + "/common/assertions.json",
		Title:       "PromptArena Assertions",
		Description: "Assertion types for PromptArena scenarios",
		Type:        "object",
	}

	return schema, nil
}

// GenerateMediaSchema generates a placeholder for media schema
func GenerateMediaSchema() (interface{}, error) {
	schema := &jsonschema.Schema{
		ID:          schemaBaseURL + "/common/media.json",
		Title:       "PromptKit Media Types",
		Description: "Media content types for multimodal scenarios",
		Type:        "object",
	}

	return schema, nil
}
