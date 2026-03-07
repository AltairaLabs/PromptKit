package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// JSONSchemaHandler validates CurrentOutput against a JSON schema.
// Params: schema map[string]any.
type JSONSchemaHandler struct{}

// Type returns the eval type identifier.
func (h *JSONSchemaHandler) Type() string { return "json_schema" }

// Eval validates the current output against the provided JSON schema.
func (h *JSONSchemaHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	schema, _ := params["schema"].(map[string]any)
	if schema == nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no schema provided",
		}, nil
	}

	allowWrapped := extractBool(params, "allow_wrapped")
	extractJSON := extractBool(params, "extract_json")

	content := evalCtx.CurrentOutput
	if allowWrapped || extractJSON {
		if extracted := extractJSONFromContent(content, allowWrapped, extractJSON); extracted != "" {
			content = extracted
		}
	}

	// Validate the output is valid JSON first
	var target any
	if parseErr := json.Unmarshal(
		[]byte(content), &target,
	); parseErr != nil {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"output is not valid JSON: %v", parseErr,
			),
		}, nil
	}

	return h.validateSchema(content, schema)
}

// validateSchema performs the JSON schema validation.
func (h *JSONSchemaHandler) validateSchema(
	content string, schema map[string]any,
) (result *evals.EvalResult, err error) {
	schemaLoader := gojsonschema.NewGoLoader(schema)
	docLoader := gojsonschema.NewStringLoader(content)

	valResult, valErr := gojsonschema.Validate(
		schemaLoader, docLoader,
	)
	if valErr != nil {
		return &evals.EvalResult{
			Type:  h.Type(),
			Error: fmt.Sprintf("schema validation error: %v", valErr),
		}, nil
	}

	if !valResult.Valid() {
		errs := make([]string, 0, len(valResult.Errors()))
		for _, e := range valResult.Errors() {
			errs = append(errs, e.String())
		}
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"schema violations: %s",
				strings.Join(errs, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "output matches JSON schema",
	}, nil
}
