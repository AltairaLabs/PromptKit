package generators

import (
	"testing"

	"github.com/invopop/jsonschema"
)

func TestGenerateArenaSchema(t *testing.T) {
	schema, err := GenerateArenaSchema()
	if err != nil {
		t.Fatalf("GenerateArenaSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateArenaSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GenerateArenaSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	if jsonSchema.Description == "" {
		t.Error("Schema description is empty")
	}

	if string(jsonSchema.ID) == "" {
		t.Error("Schema ID is empty")
	}

	expectedID := schemaBaseURL + "/arena.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}

	if len(jsonSchema.Examples) == 0 {
		t.Error("Schema has no examples")
	}
}

func TestGenerateScenarioSchema(t *testing.T) {
	schema, err := GenerateScenarioSchema()
	if err != nil {
		t.Fatalf("GenerateScenarioSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateScenarioSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GenerateScenarioSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	expectedID := schemaBaseURL + "/scenario.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}
}

func TestGenerateProviderSchema(t *testing.T) {
	schema, err := GenerateProviderSchema()
	if err != nil {
		t.Fatalf("GenerateProviderSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateProviderSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GenerateProviderSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	expectedID := schemaBaseURL + "/provider.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}
}

func TestGeneratePromptConfigSchema(t *testing.T) {
	schema, err := GeneratePromptConfigSchema()
	if err != nil {
		t.Fatalf("GeneratePromptConfigSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GeneratePromptConfigSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GeneratePromptConfigSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	expectedID := schemaBaseURL + "/promptconfig.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}
}

func TestGenerateToolSchema(t *testing.T) {
	schema, err := GenerateToolSchema()
	if err != nil {
		t.Fatalf("GenerateToolSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateToolSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GenerateToolSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	expectedID := schemaBaseURL + "/tool.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}
}

func TestGeneratePersonaSchema(t *testing.T) {
	schema, err := GeneratePersonaSchema()
	if err != nil {
		t.Fatalf("GeneratePersonaSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GeneratePersonaSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GeneratePersonaSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	expectedID := schemaBaseURL + "/persona.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}
}

func TestGenerateEvalSchema(t *testing.T) {
	schema, err := GenerateEvalSchema()
	if err != nil {
		t.Fatalf("GenerateEvalSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateEvalSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GenerateEvalSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	if jsonSchema.Description == "" {
		t.Error("Schema description is empty")
	}

	expectedID := schemaBaseURL + "/eval.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}

	if len(jsonSchema.Examples) == 0 {
		t.Error("Schema has no examples")
	}
}

func TestGenerateLoggingSchema(t *testing.T) {
	schema, err := GenerateLoggingSchema()
	if err != nil {
		t.Fatalf("GenerateLoggingSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateLoggingSchema() returned nil schema")
	}

	jsonSchema, ok := schema.(*jsonschema.Schema)
	if !ok {
		t.Fatal("GenerateLoggingSchema() did not return *jsonschema.Schema")
	}

	if jsonSchema.Title == "" {
		t.Error("Schema title is empty")
	}

	if jsonSchema.Description == "" {
		t.Error("Schema description is empty")
	}

	expectedID := schemaBaseURL + "/logging.json"
	if string(jsonSchema.ID) != expectedID {
		t.Errorf("Schema ID = %v, want %v", jsonSchema.ID, expectedID)
	}

	if len(jsonSchema.Examples) == 0 {
		t.Error("Schema has no examples")
	}
}

func TestGenerateMetadataSchema(t *testing.T) {
	schema, err := GenerateMetadataSchema()
	if err != nil {
		t.Fatalf("GenerateMetadataSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateMetadataSchema() returned nil schema")
	}
}

func TestGenerateAssertionsSchema(t *testing.T) {
	schema, err := GenerateAssertionsSchema()
	if err != nil {
		t.Fatalf("GenerateAssertionsSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateAssertionsSchema() returned nil schema")
	}
}

func TestGenerateMediaSchema(t *testing.T) {
	schema, err := GenerateMediaSchema()
	if err != nil {
		t.Fatalf("GenerateMediaSchema() error = %v", err)
	}

	if schema == nil {
		t.Fatal("GenerateMediaSchema() returned nil schema")
	}
}

func TestSchemaBaseURL(t *testing.T) {
	expected := "https://promptkit.altairalabs.ai/schemas/v1alpha1"
	if schemaBaseURL != expected {
		t.Errorf("schemaBaseURL = %v, want %v", schemaBaseURL, expected)
	}
}
