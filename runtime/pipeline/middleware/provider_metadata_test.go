package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBuildProviderRequest_MetadataCopy(t *testing.T) {
	tests := []struct {
		name              string
		executionMetadata map[string]interface{}
		expectedMetadata  map[string]interface{}
	}{
		{
			name: "copy all metadata",
			executionMetadata: map[string]interface{}{
				"key1": "value1",
				"key2": 42,
				"key3": true,
			},
			expectedMetadata: map[string]interface{}{
				"key1": "value1",
				"key2": 42,
				"key3": true,
			},
		},
		{
			name:              "nil metadata",
			executionMetadata: nil,
			expectedMetadata:  nil,
		},
		{
			name:              "empty metadata",
			executionMetadata: map[string]interface{}{},
			expectedMetadata:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := &pipeline.ExecutionContext{
				Messages: []types.Message{{Role: "user", Content: "Test"}},
				Metadata: tt.executionMetadata,
			}

			req := buildProviderRequest(execCtx, nil)

			if tt.expectedMetadata == nil {
				if len(req.Metadata) > 0 {
					t.Errorf("Expected PredictionRequest.Metadata to be nil or empty, got %v", req.Metadata)
				}
			} else {
				if req.Metadata == nil {
					t.Error("Expected PredictionRequest.Metadata to be set, but it was nil")
					return
				}

				if len(req.Metadata) != len(tt.expectedMetadata) {
					t.Errorf("PredictionRequest.Metadata length = %d, want %d", len(req.Metadata), len(tt.expectedMetadata))
				}

				for key, expectedValue := range tt.expectedMetadata {
					actualValue, exists := req.Metadata[key]
					if !exists {
						t.Errorf("Expected metadata key %q to exist in PredictionRequest", key)
						continue
					}
					if actualValue != expectedValue {
						t.Errorf("PredictionRequest.Metadata[%q] = %v, want %v", key, actualValue, expectedValue)
					}
				}
			}
		})
	}
}
