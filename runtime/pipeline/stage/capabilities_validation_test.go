package stage

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
			AudioCapabilities([]AudioFormat{AudioFormatPCM16, AudioFormatOpus}, nil, nil),
			TextCapabilities(),
		)
		want := "test: accepts=audio(pcm16|opus) produces=text"
		if got := DescribeCapabilities(stage); got != want {
			t.Errorf("DescribeCapabilities() = %q, want %q", got, want)
		}
	})

	t.Run("FormatCapable stage with no content types", func(t *testing.T) {
		stage := newMockFormatCapableStage("open", Capabilities{}, Capabilities{})
		want := "open: accepts=any produces=any"
		if got := DescribeCapabilities(stage); got != want {
			t.Errorf("DescribeCapabilities() = %q, want %q", got, want)
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

// captureWarnings routes the package logger into a buffer for the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return &buf
}

const mismatchMarker = "Pipeline format mismatch"

// A pipeline whose connected stages cannot exchange data builds, but the
// builder warns once per incompatibility, naming the edge. The warning is the
// only signal a host gets before runtime, so each mismatch kind must produce
// its own message and a compatible graph must produce none.
func TestPipelineBuilder_WarnsOnFormatMismatch(t *testing.T) {
	audio := func(c AudioCapability) Capabilities {
		return Capabilities{ContentTypes: []ContentType{ContentTypeAudio}, Audio: &c}
	}
	tests := []struct {
		name     string
		from, to Stage
		want     []string // substrings that must appear
		wantNone bool     // no mismatch warning at all
	}{
		{
			name: "compatible audio formats",
			from: newMockFormatCapableStage("stage-a", AnyCapabilities(),
				AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil)),
			to: newMockFormatCapableStage("stage-b",
				AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil), AnyCapabilities()),
			wantNone: true,
		},
		{
			name: "upstream declares nothing",
			from: NewPassthroughStage("stage-a"),
			to: newMockFormatCapableStage("stage-b",
				AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil), AnyCapabilities()),
			wantNone: true,
		},
		{
			name: "audio format",
			from: newMockFormatCapableStage("stage-a", AnyCapabilities(),
				AudioCapabilities([]AudioFormat{AudioFormatOpus}, nil, nil)),
			to: newMockFormatCapableStage("stage-b",
				AudioCapabilities([]AudioFormat{AudioFormatPCM16}, nil, nil), AnyCapabilities()),
			want: []string{"audio format incompatibility", "from_stage=stage-a", "to_stage=stage-b",
				"from_produces=[opus]", "to_accepts=[pcm16]"},
		},
		{
			name: "content type",
			from: newMockFormatCapableStage("stage-a", AnyCapabilities(),
				Capabilities{ContentTypes: []ContentType{ContentTypeAudio}}),
			to: newMockFormatCapableStage("stage-b",
				Capabilities{ContentTypes: []ContentType{ContentTypeText}}, AnyCapabilities()),
			want: []string{"content type incompatibility", "from_stage=stage-a", "to_stage=stage-b",
				"from_produces=[audio]", "to_accepts=[text]"},
		},
		{
			name: "sample rate",
			from: newMockFormatCapableStage("stage-a", AnyCapabilities(),
				audio(AudioCapability{SampleRates: []int{44100}})),
			to: newMockFormatCapableStage("stage-b",
				audio(AudioCapability{SampleRates: []int{16000}}), AnyCapabilities()),
			want: []string{"sample rate incompatibility", "from_produces=[44100]", "to_accepts=[16000]"},
		},
		{
			name: "channel count",
			from: newMockFormatCapableStage("stage-a", AnyCapabilities(),
				audio(AudioCapability{Channels: []int{1}})),
			to: newMockFormatCapableStage("stage-b",
				audio(AudioCapability{Channels: []int{2}}), AnyCapabilities()),
			want: []string{"channel count incompatibility", "from_produces=[1]", "to_accepts=[2]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureWarnings(t)
			_, err := NewPipelineBuilder().Chain(tt.from, tt.to).Build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			log := buf.String()
			if tt.wantNone {
				if strings.Contains(log, mismatchMarker) {
					t.Fatalf("compatible stages logged a mismatch: %s", log)
				}
				return
			}
			if n := strings.Count(log, mismatchMarker); n != 1 {
				t.Fatalf("want exactly 1 mismatch warning, got %d: %s", n, log)
			}
			for _, w := range tt.want {
				if !strings.Contains(log, w) {
					t.Errorf("warning missing %q: %s", w, log)
				}
			}
		})
	}
}

// ValidateCapabilities checks only the edges it is given: a mismatched pair
// with no edge between them is not a mismatch.
func TestValidateCapabilities_OnlyChecksConnectedStages(t *testing.T) {
	buf := captureWarnings(t)
	a := newMockFormatCapableStage("stage-a", AnyCapabilities(),
		Capabilities{ContentTypes: []ContentType{ContentTypeAudio}})
	b := newMockFormatCapableStage("stage-b",
		Capabilities{ContentTypes: []ContentType{ContentTypeText}}, AnyCapabilities())

	ValidateCapabilities([]Stage{a, b}, map[string][]string{"stage-b": {"stage-a"}, "missing": {"stage-a"}})
	if strings.Contains(buf.String(), mismatchMarker) {
		t.Fatalf("reverse edge b->a is compatible; got warning: %s", buf.String())
	}

	ValidateCapabilities([]Stage{a, b}, map[string][]string{"stage-a": {"stage-b"}})
	if !strings.Contains(buf.String(), "content type incompatibility") {
		t.Fatalf("edge a->b should warn; got: %s", buf.String())
	}
}
