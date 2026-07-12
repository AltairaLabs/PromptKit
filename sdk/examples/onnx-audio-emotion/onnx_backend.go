package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	ort "github.com/yalue/onnxruntime_go"
)

// defaultEmotionLabels is the output-index label order for the default
// wav2vec2 SER model (superb/wav2vec2-base-superb-er). VERIFY against the
// model's config.json id2label when swapping models.
var defaultEmotionLabels = []string{"neu", "hap", "ang", "sad"}

// onnxConfig configures the ONNX audio-emotion backend.
type onnxConfig struct {
	LibPath    string   // path to libonnxruntime.{so,dylib,dll}
	ModelPath  string   // path to the .onnx model
	InputName  string   // model input tensor name (default "input_values")
	OutputName string   // model output tensor name (default "logits")
	Labels     []string // output-index label order
}

// ortInitOnce guards process-global ONNX Runtime environment setup.
var ortInitOnce sync.Once

// onnxAudioClassifier implements classify.AudioClassifier by running a
// wav2vec2 speech-emotion-recognition model via ONNX Runtime. It is the
// worked demonstration of the classify pluggability seam: the runtime and
// SDK know only the classify.AudioClassifier interface; the cgo/ONNX
// dependency lives entirely in this example module.
type onnxAudioClassifier struct {
	session    *ort.DynamicAdvancedSession
	inputName  string
	outputName string
	labels     []string
}

// compile-time proof the example satisfies the runtime interface.
var _ classify.AudioClassifier = (*onnxAudioClassifier)(nil)

func newONNXAudioClassifier(cfg onnxConfig) (*onnxAudioClassifier, error) {
	if cfg.InputName == "" {
		cfg.InputName = "input_values"
	}
	if cfg.OutputName == "" {
		cfg.OutputName = "logits"
	}
	if len(cfg.Labels) == 0 {
		cfg.Labels = defaultEmotionLabels
	}
	var initErr error
	ortInitOnce.Do(func() {
		if cfg.LibPath != "" {
			ort.SetSharedLibraryPath(cfg.LibPath)
		}
		initErr = ort.InitializeEnvironment()
	})
	if initErr != nil {
		return nil, fmt.Errorf("initialize ONNX Runtime (is libonnxruntime present? run `make setup`): %w", initErr)
	}
	session, err := ort.NewDynamicAdvancedSession(
		cfg.ModelPath,
		[]string{cfg.InputName},
		[]string{cfg.OutputName},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("load ONNX model %q: %w", cfg.ModelPath, err)
	}
	return &onnxAudioClassifier{
		session:    session,
		inputName:  cfg.InputName,
		outputName: cfg.OutputName,
		labels:     cfg.Labels,
	}, nil
}

// ClassifyAudio decodes raw WAV bytes, normalizes the waveform, runs the
// SER model, and returns softmaxed label scores. The eval handler hands us
// the raw audio bytes; owning decode->normalize->run->softmax here is
// exactly what makes this a drop-in classify.AudioClassifier.
func (c *onnxAudioClassifier) ClassifyAudio(
	_ context.Context, audio []byte, _ classify.AudioOptions,
) ([]classify.LabelScore, error) {
	samples, _, err := decodeWAV(audio)
	if err != nil {
		return nil, fmt.Errorf("decode audio: %w", err)
	}
	x := normalize(samples)

	inputTensor, err := ort.NewTensor(ort.NewShape(1, int64(len(x))), x)
	if err != nil {
		return nil, fmt.Errorf("build input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(c.labels))))
	if err != nil {
		return nil, fmt.Errorf("build output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	if err := c.session.Run([]ort.Value{inputTensor}, []ort.Value{outputTensor}); err != nil {
		return nil, fmt.Errorf("onnx run: %w", err)
	}
	return labelScores(outputTensor.GetData(), c.labels)
}

// Close releases the ONNX session.
func (c *onnxAudioClassifier) Close() error {
	if c.session != nil {
		return c.session.Destroy()
	}
	return nil
}
