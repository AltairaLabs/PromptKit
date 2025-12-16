package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// MetadataInjectionStage injects metadata into stream element metadata.
type MetadataInjectionStage struct {
	stage.BaseStage
	metadata map[string]interface{}
}

// NewMetadataInjectionStage creates a new metadata injection stage.
func NewMetadataInjectionStage(metadata map[string]interface{}) *MetadataInjectionStage {
	return &MetadataInjectionStage{
		BaseStage: stage.NewBaseStage("metadata_injection", stage.StageTypeTransform),
		metadata:  metadata,
	}
}

// Process adds metadata to all elements.
func (s *MetadataInjectionStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Skip if no metadata to inject
		if len(s.metadata) == 0 {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Ensure metadata exists
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}

		// Copy metadata to element
		for k, v := range s.metadata {
			elem.Metadata[k] = v
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
