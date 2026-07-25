package classify

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
)

// Aggregation strategies for the decomposing video classifier. They
// name how per-frame (and audio-window) label scores collapse into a
// single result.
const (
	// AggregationMax takes each label's highest score across frames —
	// the default, since moderation cares about the worst frame.
	AggregationMax = "max"
	// AggregationMean averages each label's score across all frames
	// (absent-in-a-frame counts as zero).
	AggregationMean = "mean"
	// AggregationVote scores each label by the fraction of frames whose
	// top label it was.
	AggregationVote = "vote"
)

// defaultFrameConcurrency bounds parallel per-frame classification.
const defaultFrameConcurrency = 4

// DecomposeConfig configures a decomposing video classifier: which
// sub-models to use, how to sample, and how to aggregate. Per-call
// VideoOptions override the sampling cadence, model, and aggregation.
type DecomposeConfig struct {
	// ImageModel is the default model id passed to the ImageClassifier
	// for each frame. VideoOptions.Model overrides it per call.
	ImageModel string

	// AudioModel is the model id passed to the AudioClassifier when
	// ExtractAudio is set and an audio track is present.
	AudioModel string

	// Aggregation is the default strategy (max|mean|vote).
	// VideoOptions.Aggregation overrides it. Empty ⇒ AggregationMax.
	Aggregation string

	// FramesPerSecond is the default sampling cadence.
	// VideoOptions.FrameSampleRate overrides it. 0 ⇒ sampler default.
	FramesPerSecond float64

	// MaxFrames caps sampled frames. 0 ⇒ sampler default.
	MaxFrames int

	// MaxConcurrency bounds parallel frame classification.
	// 0 ⇒ defaultFrameConcurrency.
	MaxConcurrency int
}

// decomposingVideoClassifier implements VideoClassifier by sampling
// frames from a video, classifying each still with an ImageClassifier,
// optionally classifying the audio track with an AudioClassifier, and
// aggregating the per-modality scores into one ranked result.
//
// Safe for concurrent use: the sampler and sub-classifiers must be too
// (they are, per their interface contracts).
type decomposingVideoClassifier struct {
	sampler framesample.Sampler
	image   ImageClassifier
	audio   AudioClassifier // optional; nil ⇒ frames only
	cfg     DecomposeConfig
}

// NewDecomposingVideoClassifier composes a VideoClassifier from a frame
// sampler, an ImageClassifier (required), and an optional
// AudioClassifier. It holds the classifier instances directly, so it is
// registry-agnostic and trivial to unit-test with fakes.
func NewDecomposingVideoClassifier(
	sampler framesample.Sampler,
	image ImageClassifier,
	audio AudioClassifier,
	cfg DecomposeConfig,
) (VideoClassifier, error) {
	if sampler == nil {
		return nil, fmt.Errorf("classify: decomposing video classifier requires a frame sampler")
	}
	if image == nil {
		return nil, fmt.Errorf("classify: decomposing video classifier requires an image classifier")
	}
	if cfg.Aggregation == "" {
		cfg.Aggregation = AggregationMax
	}
	return &decomposingVideoClassifier{sampler: sampler, image: image, audio: audio, cfg: cfg}, nil
}

// ClassifyVideo samples frames, classifies each (and optionally the
// audio track), and aggregates the scores. ErrUnsupportedContainer from
// the sampler propagates unchanged so callers can map it to a skip.
func (d *decomposingVideoClassifier) ClassifyVideo(
	ctx context.Context, video []byte, opts VideoOptions,
) ([]LabelScore, error) {
	extractAudio := opts.ExtractAudio && d.audio != nil
	res, err := d.sampler.Sample(ctx, video, framesample.SampleOptions{
		MIMEType:        opts.MIMEType,
		FramesPerSecond: pickFloat(opts.FrameSampleRate, d.cfg.FramesPerSecond),
		MaxFrames:       d.cfg.MaxFrames,
		ExtractAudio:    extractAudio,
	})
	if err != nil {
		return nil, err
	}
	if len(res.Frames) == 0 {
		return nil, fmt.Errorf("classify: no frames decoded from video")
	}

	imageModel := opts.Model
	if imageModel == "" {
		imageModel = d.cfg.ImageModel
	}
	groups, err := d.classifyFrames(ctx, res.Frames, imageModel)
	if err != nil {
		return nil, err
	}

	if extractAudio && len(res.AudioPCM) > 0 {
		audioScores, audioErr := d.audio.ClassifyAudio(ctx, res.AudioPCM, AudioOptions{
			Model:    d.cfg.AudioModel,
			MIMEType: res.AudioMIME,
		})
		if audioErr != nil {
			return nil, fmt.Errorf("classify: audio track: %w", audioErr)
		}
		groups = append(groups, audioScores)
	}

	strategy := opts.Aggregation
	if strategy == "" {
		strategy = d.cfg.Aggregation
	}
	return aggregateScores(groups, strategy), nil
}

// classifyFrames runs each frame through the ImageClassifier with bounded
// concurrency, preserving frame order. The first error cancels the rest.
func (d *decomposingVideoClassifier) classifyFrames(
	ctx context.Context, frames []framesample.Frame, model string,
) ([][]LabelScore, error) {
	conc := d.cfg.MaxConcurrency
	if conc <= 0 {
		conc = defaultFrameConcurrency
	}
	results := make([][]LabelScore, len(frames))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := range frames {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			scores, err := d.image.ClassifyImage(ctx, frames[i].Data, ImageOptions{
				Model:    model,
				MIMEType: frames[i].MIMEType,
			})
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("classify: frame %d: %w", i, err)
					cancel()
				}
				mu.Unlock()
				return
			}
			results[i] = scores
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// pickFloat returns override when positive, else fallback.
func pickFloat(override, fallback float64) float64 {
	if override > 0 {
		return override
	}
	return fallback
}

// aggregateScores collapses per-frame/audio label-score groups into a
// single ranked slice using the named strategy. Unknown strategies fall
// back to max. Labels are matched case-insensitively; the first-seen
// casing is preserved in the output.
func aggregateScores(groups [][]LabelScore, strategy string) []LabelScore {
	total := len(groups)
	if total == 0 {
		return nil
	}

	switch strategy {
	case AggregationMean:
		return aggregateMean(groups, total)
	case AggregationVote:
		return aggregateVote(groups, total)
	default: // AggregationMax and unknown
		return aggregateMax(groups)
	}
}

// labelKey normalizes a label for case-insensitive grouping.
func labelKey(label string) string { return strings.ToLower(label) }

// canonicalLabels tracks the first-seen display casing for each key so
// aggregated output uses stable, human-readable labels.
type labelAccumulator struct {
	display map[string]string
	order   []string
}

func newLabelAccumulator() *labelAccumulator {
	return &labelAccumulator{display: make(map[string]string)}
}

func (a *labelAccumulator) note(label string) string {
	key := labelKey(label)
	if _, ok := a.display[key]; !ok {
		a.display[key] = label
		a.order = append(a.order, key)
	}
	return key
}

func aggregateMax(groups [][]LabelScore) []LabelScore {
	acc := newLabelAccumulator()
	best := make(map[string]float64)
	for _, g := range groups {
		for _, s := range g {
			key := acc.note(s.Label)
			if s.Score > best[key] {
				best[key] = s.Score
			}
		}
	}
	return acc.sorted(best)
}

func aggregateMean(groups [][]LabelScore, total int) []LabelScore {
	acc := newLabelAccumulator()
	sum := make(map[string]float64)
	for _, g := range groups {
		for _, s := range g {
			key := acc.note(s.Label)
			sum[key] += s.Score
		}
	}
	mean := make(map[string]float64, len(sum))
	for key, v := range sum {
		mean[key] = v / float64(total)
	}
	return acc.sorted(mean)
}

func aggregateVote(groups [][]LabelScore, total int) []LabelScore {
	acc := newLabelAccumulator()
	votes := make(map[string]float64)
	for _, g := range groups {
		if top, ok := topLabel(g); ok {
			key := acc.note(top)
			votes[key]++
		}
	}
	for key := range votes {
		votes[key] /= float64(total)
	}
	return acc.sorted(votes)
}

// topLabel returns the highest-scoring label in a group.
func topLabel(g []LabelScore) (string, bool) {
	if len(g) == 0 {
		return "", false
	}
	best := g[0]
	for _, s := range g[1:] {
		if s.Score > best.Score {
			best = s
		}
	}
	return best.Label, true
}

// sorted materializes the accumulated scores as a slice ranked by
// descending score, with label as a stable tiebreaker.
func (a *labelAccumulator) sorted(scores map[string]float64) []LabelScore {
	out := make([]LabelScore, 0, len(a.order))
	for _, key := range a.order {
		out = append(out, LabelScore{Label: a.display[key], Score: scores[key]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Label < out[j].Label
	})
	return out
}
