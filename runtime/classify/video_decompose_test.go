package classify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
)

// --- fakes ---------------------------------------------------------------

type fakeSampler struct {
	res     framesample.SampleResult
	err     error
	mu      sync.Mutex
	gotOpts framesample.SampleOptions
}

func (f *fakeSampler) Sample(
	_ context.Context, _ []byte, opts framesample.SampleOptions,
) (framesample.SampleResult, error) {
	f.mu.Lock()
	f.gotOpts = opts
	f.mu.Unlock()
	return f.res, f.err
}

type fakeImageClassifier struct {
	byData map[string][]LabelScore
	err    error
	mu     sync.Mutex
	models map[string]int
}

func (f *fakeImageClassifier) ClassifyImage(
	_ context.Context, img []byte, opts ImageOptions,
) ([]LabelScore, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	if f.models == nil {
		f.models = map[string]int{}
	}
	f.models[opts.Model]++
	f.mu.Unlock()
	return f.byData[string(img)], nil
}

type fakeAudioClassifier struct {
	scores []LabelScore
	err    error
	gotPCM []byte
}

func (f *fakeAudioClassifier) ClassifyAudio(
	_ context.Context, audio []byte, _ AudioOptions,
) ([]LabelScore, error) {
	f.gotPCM = audio
	return f.scores, f.err
}

// framesFrom builds sampler frames with distinct data payloads.
func framesFrom(datas ...string) []framesample.Frame {
	out := make([]framesample.Frame, len(datas))
	for i, d := range datas {
		out[i] = framesample.Frame{Data: []byte(d), MIMEType: "image/png", Timestamp: time.Duration(i) * time.Second}
	}
	return out
}

func twoFrameSetup() (*fakeSampler, *fakeImageClassifier) {
	sampler := &fakeSampler{res: framesample.SampleResult{Frames: framesFrom("f0", "f1")}}
	img := &fakeImageClassifier{byData: map[string][]LabelScore{
		"f0": {{Label: "nsfw", Score: 0.9}, {Label: "safe", Score: 0.1}},
		"f1": {{Label: "nsfw", Score: 0.2}, {Label: "safe", Score: 0.8}},
	}}
	return sampler, img
}

func scoreOf(scores []LabelScore, label string) (float64, bool) {
	for _, s := range scores {
		if s.Label == label {
			return s.Score, true
		}
	}
	return 0, false
}

// --- tests ---------------------------------------------------------------

func TestNewDecomposingVideoClassifier_Validation(t *testing.T) {
	img := &fakeImageClassifier{}
	if _, err := NewDecomposingVideoClassifier(nil, img, nil, DecomposeConfig{}); err == nil {
		t.Error("expected error for nil sampler")
	}
	if _, err := NewDecomposingVideoClassifier(&fakeSampler{}, nil, nil, DecomposeConfig{}); err == nil {
		t.Error("expected error for nil image classifier")
	}
}

func TestClassifyVideo_AggregateMax(t *testing.T) {
	sampler, img := twoFrameSetup()
	vc, err := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	scores, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{})
	if err != nil {
		t.Fatalf("ClassifyVideo: %v", err)
	}
	// Default aggregation is max: nsfw=max(0.9,0.2)=0.9, safe=max(0.1,0.8)=0.8.
	if got, _ := scoreOf(scores, "nsfw"); got != 0.9 {
		t.Errorf("nsfw = %v, want 0.9", got)
	}
	if got, _ := scoreOf(scores, "safe"); got != 0.8 {
		t.Errorf("safe = %v, want 0.8", got)
	}
	// Ranked descending: nsfw (0.9) before safe (0.8).
	if scores[0].Label != "nsfw" {
		t.Errorf("top label = %q, want nsfw", scores[0].Label)
	}
}

func TestClassifyVideo_AggregateMean(t *testing.T) {
	sampler, img := twoFrameSetup()
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{Aggregation: AggregationMean})

	scores, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// mean over 2 frames: nsfw=(0.9+0.2)/2=0.55, safe=(0.1+0.8)/2=0.45.
	if got, _ := scoreOf(scores, "nsfw"); !approx(got, 0.55) {
		t.Errorf("nsfw mean = %v, want 0.55", got)
	}
	if got, _ := scoreOf(scores, "safe"); !approx(got, 0.45) {
		t.Errorf("safe mean = %v, want 0.45", got)
	}
}

func TestClassifyVideo_AggregateVote(t *testing.T) {
	sampler, img := twoFrameSetup()
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{Aggregation: AggregationVote})

	scores, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// f0 top=nsfw, f1 top=safe → each 1/2.
	if got, _ := scoreOf(scores, "nsfw"); !approx(got, 0.5) {
		t.Errorf("nsfw vote = %v, want 0.5", got)
	}
	if got, _ := scoreOf(scores, "safe"); !approx(got, 0.5) {
		t.Errorf("safe vote = %v, want 0.5", got)
	}
}

func TestClassifyVideo_PerCallAggregationOverride(t *testing.T) {
	sampler, img := twoFrameSetup()
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{Aggregation: AggregationMax})

	scores, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{Aggregation: AggregationMean})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := scoreOf(scores, "nsfw"); !approx(got, 0.55) {
		t.Errorf("per-call mean override: nsfw = %v, want 0.55", got)
	}
}

func TestClassifyVideo_WithAudio(t *testing.T) {
	sampler, img := twoFrameSetup()
	sampler.res.AudioPCM = []byte("pcm")
	sampler.res.AudioMIME = "audio/wav"
	audio := &fakeAudioClassifier{scores: []LabelScore{{Label: "angry", Score: 0.6}}}
	vc, _ := NewDecomposingVideoClassifier(sampler, img, audio, DecomposeConfig{Aggregation: AggregationMax})

	scores, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{ExtractAudio: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := scoreOf(scores, "angry"); !ok || got != 0.6 {
		t.Errorf("angry = %v (found %v), want 0.6", got, ok)
	}
	if string(audio.gotPCM) != "pcm" {
		t.Errorf("audio classifier got PCM %q, want pcm", audio.gotPCM)
	}
	if !sampler.gotOpts.ExtractAudio {
		t.Error("sampler should have been asked to extract audio")
	}
}

func TestClassifyVideo_AudioIgnoredWhenNoAudioClassifier(t *testing.T) {
	sampler, img := twoFrameSetup()
	sampler.res.AudioPCM = []byte("pcm")
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{})

	_, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{ExtractAudio: true})
	if err != nil {
		t.Fatal(err)
	}
	// With no audio classifier, the sampler must not be asked for audio.
	if sampler.gotOpts.ExtractAudio {
		t.Error("sampler asked to extract audio despite nil audio classifier")
	}
}

func TestClassifyVideo_UnsupportedContainerPropagates(t *testing.T) {
	sampler := &fakeSampler{err: framesample.ErrUnsupportedContainer}
	img := &fakeImageClassifier{}
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{})

	_, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{})
	if !errors.Is(err, framesample.ErrUnsupportedContainer) {
		t.Fatalf("err = %v, want ErrUnsupportedContainer", err)
	}
}

func TestClassifyVideo_NoFrames(t *testing.T) {
	sampler := &fakeSampler{res: framesample.SampleResult{}}
	img := &fakeImageClassifier{}
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{})

	_, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{})
	if err == nil {
		t.Fatal("expected error when no frames decoded")
	}
}

func TestClassifyVideo_FrameErrorPropagates(t *testing.T) {
	sampler, _ := twoFrameSetup()
	img := &fakeImageClassifier{err: errors.New("classify boom")}
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{})

	_, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{})
	if err == nil {
		t.Fatal("expected frame classification error to propagate")
	}
}

func TestClassifyVideo_ModelAndCadencePlumbing(t *testing.T) {
	sampler, img := twoFrameSetup()
	vc, _ := NewDecomposingVideoClassifier(sampler, img, nil, DecomposeConfig{
		ImageModel: "cfg-model", FramesPerSecond: 3,
	})

	// Per-call Model overrides the config model; FrameSampleRate overrides cadence.
	_, err := vc.ClassifyVideo(context.Background(), []byte("vid"), VideoOptions{
		Model: "call-model", FrameSampleRate: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if img.models["call-model"] != 2 {
		t.Errorf("image model calls: got %v, want call-model x2", img.models)
	}
	if sampler.gotOpts.FramesPerSecond != 5 {
		t.Errorf("sampler fps = %v, want 5 (per-call override)", sampler.gotOpts.FramesPerSecond)
	}
}

func approx(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}
