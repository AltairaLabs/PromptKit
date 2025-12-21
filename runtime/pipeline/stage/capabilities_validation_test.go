package stage

import (
	"context"
	"testing"
)

// mockFormatCapableStage is a test stage that implements FormatCapable
type mockFormatCapableStage struct {
	BaseStage
	inputCaps  Capabilities
	outputCaps Capabilities
}

func newMockFormatCapableStage(name string, input, output Capabilities) *mockFormatCapableStage {
	return &mockFormatCapableStage{
		BaseStage:  NewBaseStage(name, StageTypeTransform),
		inputCaps:  input,
		outputCaps: output,
	}
}

func (m *mockFormatCapableStage) InputCapabilities() Capabilities {
	return m.inputCaps
}

func (m *mockFormatCapableStage) OutputCapabilities() Capabilities {
	return m.outputCaps
}

func (m *mockFormatCapableStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestValidateCapabilities_Compatible(t *testing.T) {
	// Two stages with compatible formats - should not log warnings
	stageA := newMockFormatCapableStage("stage-a",
		AnyCapabilities(),
		AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil),
	)
	stageB := newMockFormatCapableStage("stage-b",
		AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil),
		AnyCapabilities(),
	)

	stages := []Stage{stageA, stageB}
	edges := map[string][]string{"stage-a": {"stage-b"}}

	// This should not panic or error
	ValidateCapabilities(stages, edges)
}

func TestValidateCapabilities_Incompatible(t *testing.T) {
	// Two stages with incompatible formats - should log warnings
	// (We can't easily verify the log output in unit tests, but we verify no panic)
	stageA := newMockFormatCapableStage("stage-a",
		AnyCapabilities(),
		AudioCapabilities([]AudioFormat{AudioFormatOpus}, nil, nil),
	)
	stageB := newMockFormatCapableStage("stage-b",
		AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil), // Only accepts PCM16
		AnyCapabilities(),
	)

	stages := []Stage{stageA, stageB}
	edges := map[string][]string{"stage-a": {"stage-b"}}

	// This should log a warning but not panic
	ValidateCapabilities(stages, edges)
}

func TestValidateCapabilities_MixedStages(t *testing.T) {
	// Mix of FormatCapable and non-FormatCapable stages
	stageA := NewPassthroughStage("stage-a") // Does not implement FormatCapable
	stageB := newMockFormatCapableStage("stage-b",
		AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil),
		AnyCapabilities(),
	)

	stages := []Stage{stageA, stageB}
	edges := map[string][]string{"stage-a": {"stage-b"}}

	// Should handle mixed stages gracefully
	ValidateCapabilities(stages, edges)
}

func TestValidateCapabilities_ContentTypeMismatch(t *testing.T) {
	// Stage A produces audio, Stage B only accepts text
	stageA := newMockFormatCapableStage("stage-a",
		AnyCapabilities(),
		Capabilities{ContentTypes: []ContentType{ContentTypeAudio}},
	)
	stageB := newMockFormatCapableStage("stage-b",
		Capabilities{ContentTypes: []ContentType{ContentTypeText}},
		AnyCapabilities(),
	)

	stages := []Stage{stageA, stageB}
	edges := map[string][]string{"stage-a": {"stage-b"}}

	// Should log warning about content type mismatch
	ValidateCapabilities(stages, edges)
}

func TestValidateCapabilities_SampleRateMismatch(t *testing.T) {
	// Stage A produces 44100Hz, Stage B only accepts 16000Hz
	stageA := newMockFormatCapableStage("stage-a",
		AnyCapabilities(),
		Capabilities{
			ContentTypes: []ContentType{ContentTypeAudio},
			Audio:        &AudioCapability{SampleRates: []int{44100}},
		},
	)
	stageB := newMockFormatCapableStage("stage-b",
		Capabilities{
			ContentTypes: []ContentType{ContentTypeAudio},
			Audio:        &AudioCapability{SampleRates: []int{16000}},
		},
		AnyCapabilities(),
	)

	stages := []Stage{stageA, stageB}
	edges := map[string][]string{"stage-a": {"stage-b"}}

	// Should log warning about sample rate mismatch
	ValidateCapabilities(stages, edges)
}

func TestDescribeCapabilities(t *testing.T) {
	t.Run("non-FormatCapable stage", func(t *testing.T) {
		stage := NewPassthroughStage("test")
		desc := DescribeCapabilities(stage)
		if desc != "test: no format capabilities declared (accepts any)" {
			t.Errorf("Unexpected description: %s", desc)
		}
	})

	t.Run("FormatCapable stage", func(t *testing.T) {
		stage := newMockFormatCapableStage("test",
			AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil),
			TextCapabilities(),
		)
		desc := DescribeCapabilities(stage)
		// Just verify it doesn't panic and returns non-empty
		if desc == "" {
			t.Error("Expected non-empty description")
		}
		if len(desc) < 10 {
			t.Errorf("Description seems too short: %s", desc)
		}
	})
}

func TestContentTypesCompatible(t *testing.T) {
	tests := []struct {
		name       string
		output     []ContentType
		input      []ContentType
		compatible bool
	}{
		{"empty output", []ContentType{}, []ContentType{ContentTypeText}, true},
		{"empty input", []ContentType{ContentTypeText}, []ContentType{}, true},
		{"both empty", []ContentType{}, []ContentType{}, true},
		{"exact match", []ContentType{ContentTypeAudio}, []ContentType{ContentTypeAudio}, true},
		{"output any", []ContentType{ContentTypeAny}, []ContentType{ContentTypeText}, true},
		{"input any", []ContentType{ContentTypeText}, []ContentType{ContentTypeAny}, true},
		{"no match", []ContentType{ContentTypeAudio}, []ContentType{ContentTypeText}, false},
		{
			"multiple - match",
			[]ContentType{ContentTypeAudio, ContentTypeText},
			[]ContentType{ContentTypeText, ContentTypeVideo},
			true,
		},
		{
			"multiple - no match",
			[]ContentType{ContentTypeAudio},
			[]ContentType{ContentTypeText, ContentTypeVideo},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contentTypesCompatible(tt.output, tt.input); got != tt.compatible {
				t.Errorf("contentTypesCompatible() = %v, want %v", got, tt.compatible)
			}
		})
	}
}

func TestAudioFormatsOverlap(t *testing.T) {
	tests := []struct {
		name    string
		a       []AudioFormat
		b       []AudioFormat
		overlap bool
	}{
		{"empty a", []AudioFormat{}, []AudioFormat{AudioFormatPCM16}, true},
		{"empty b", []AudioFormat{AudioFormatPCM16}, []AudioFormat{}, true},
		{"both empty", []AudioFormat{}, []AudioFormat{}, true},
		{"match", []AudioFormat{AudioFormatPCM16}, []AudioFormat{AudioFormatPCM16}, true},
		{"no match", []AudioFormat{AudioFormatPCM16}, []AudioFormat{AudioFormatOpus}, false},
		{"multiple match", []AudioFormat{AudioFormatPCM16, AudioFormatOpus}, []AudioFormat{AudioFormatOpus}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audioFormatsOverlap(tt.a, tt.b); got != tt.overlap {
				t.Errorf("audioFormatsOverlap() = %v, want %v", got, tt.overlap)
			}
		})
	}
}

func TestIntsOverlap(t *testing.T) {
	tests := []struct {
		name    string
		a       []int
		b       []int
		overlap bool
	}{
		{"empty a", []int{}, []int{16000}, true},
		{"empty b", []int{16000}, []int{}, true},
		{"both empty", []int{}, []int{}, true},
		{"match", []int{16000}, []int{16000}, true},
		{"no match", []int{16000}, []int{44100}, false},
		{"multiple match", []int{16000, 24000}, []int{24000, 44100}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := intsOverlap(tt.a, tt.b); got != tt.overlap {
				t.Errorf("intsOverlap() = %v, want %v", got, tt.overlap)
			}
		})
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name   string
		strs   []string
		sep    string
		expect string
	}{
		{"empty", []string{}, "|", ""},
		{"single", []string{"a"}, "|", "a"},
		{"multiple", []string{"a", "b", "c"}, "|", "a|b|c"},
		{"different sep", []string{"x", "y"}, ",", "x,y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinStrings(tt.strs, tt.sep); got != tt.expect {
				t.Errorf("joinStrings() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestDescribeAudioCapability(t *testing.T) {
	tests := []struct {
		name   string
		audio  *AudioCapability
		expect string
	}{
		{"nil", nil, ""},
		{"empty formats", &AudioCapability{}, ""},
		{"with formats", &AudioCapability{Formats: []AudioFormat{AudioFormatPCM16, AudioFormatOpus}}, "pcm16|opus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeAudioCapability(tt.audio); got != tt.expect {
				t.Errorf("describeAudioCapability() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestCheckAudioCompatibility_ChannelMismatch(t *testing.T) {
	// This test verifies the channel mismatch logging path
	output := &AudioCapability{Channels: []int{1}}
	input := &AudioCapability{Channels: []int{2}}

	// Should log warning but not panic
	checkAudioCompatibility("from", "to", output, input)
}

func TestPipelineBuilder_ValidatesCapabilities(t *testing.T) {
	// Build a pipeline with incompatible stages
	// This should build successfully but log warnings
	stageA := newMockFormatCapableStage("stage-a",
		AnyCapabilities(),
		Capabilities{ContentTypes: []ContentType{ContentTypeAudio}},
	)
	stageB := newMockFormatCapableStage("stage-b",
		Capabilities{ContentTypes: []ContentType{ContentTypeText}}, // Incompatible!
		AnyCapabilities(),
	)

	pipeline, err := NewPipelineBuilder().
		Chain(stageA, stageB).
		Build()

	// Should build successfully (validation only logs warnings)
	if err != nil {
		t.Fatalf("Pipeline build failed: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Pipeline is nil")
	}
}
