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

func TestNewReflectorDefaults(t *testing.T) {
	r := newReflector("")
	if r.FieldNameTag != "yaml" {
		t.Errorf("newReflector(\"\").FieldNameTag = %q, want \"yaml\"", r.FieldNameTag)
	}
	if r.AllowAdditionalProperties {
		t.Error("expected AllowAdditionalProperties to be false")
	}
	if !r.ExpandedStruct {
		t.Error("expected ExpandedStruct to be true")
	}
}

func TestNewReflectorCustomTag(t *testing.T) {
	r := newReflector("json")
	if r.FieldNameTag != "json" {
		t.Errorf("newReflector(\"json\").FieldNameTag = %q, want \"json\"", r.FieldNameTag)
	}
}

func TestGenerateHelper(t *testing.T) {
	type testStruct struct {
		Name string `yaml:"name"`
	}

	customized := false
	result, err := Generate(&SchemaConfig{
		Target:      &testStruct{},
		Filename:    "test.json",
		Title:       "Test Schema",
		Description: "A test schema",
		Customize: func(s *jsonschema.Schema) {
			customized = true
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	schema, ok := result.(*jsonschema.Schema)
	if !ok {
		t.Fatal("Generate() did not return *jsonschema.Schema")
	}

	if schema.Title != "Test Schema" {
		t.Errorf("Title = %q, want %q", schema.Title, "Test Schema")
	}

	if schema.Description != "A test schema" {
		t.Errorf("Description = %q, want %q", schema.Description, "A test schema")
	}

	expectedID := schemaBaseURL + "/test.json"
	if string(schema.ID) != expectedID {
		t.Errorf("ID = %q, want %q", schema.ID, expectedID)
	}

	if string(schema.Version) != "https://json-schema.org/draft-07/schema" {
		t.Errorf("Version = %q, want draft-07", schema.Version)
	}

	if !customized {
		t.Error("Customize callback was not called")
	}
}

func TestGenerateHelperNoCustomize(t *testing.T) {
	type testStruct struct {
		Name string `yaml:"name"`
	}

	result, err := Generate(&SchemaConfig{
		Target:      &testStruct{},
		Filename:    "test.json",
		Title:       "Test Schema",
		Description: "A test schema",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result == nil {
		t.Fatal("Generate() returned nil")
	}
}

func TestGenerateHelperAddsSchemaField(t *testing.T) {
	type testStruct struct {
		Name string `yaml:"name"`
	}

	result, err := Generate(&SchemaConfig{
		Target:      &testStruct{},
		Filename:    "test.json",
		Title:       "Test",
		Description: "Test",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	schema := result.(*jsonschema.Schema)

	// The schema should have $schema as a property
	if schema.Properties == nil {
		t.Fatal("schema.Properties is nil")
	}

	schemaProp, ok := schema.Properties.Get("$schema")
	if !ok {
		t.Error("$schema property not found in schema")
	}

	if schemaProp == nil {
		t.Error("$schema property is nil")
	}
}
