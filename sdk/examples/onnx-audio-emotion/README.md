# ONNX Audio-Emotion Example

Demonstrates plugging a **custom `classify` backend** into the PromptKit SDK.
This example implements a speech-emotion classifier backed by an ONNX
[wav2vec2](https://huggingface.co/onnx-community/wav2vec2-base-Speech_Emotion_Recognition-ONNX)
model, registers it with `sdk.WithClassifier`, and scores the caller's audio
with the built-in `audio_emotion` eval handler — **no runtime changes, no
Hugging Face token, fully offline.**

The whole point: the PromptKit runtime is deliberately CGO-free. The native
ONNX dependency lives entirely in *this example module* (which is **not** part
of the workspace `go.work`), so the runtime stays clean. ONNX is just one
worked demonstration of the extension point — you could plug in Replicate, a
local model server, Core ML, or anything else the same way.

## How it works

`onnxAudioClassifier` implements `classify.AudioClassifier`:

```
ClassifyAudio(ctx, wavBytes, opts):
  samples := decodeWAV(wavBytes)   // require 16 kHz mono PCM16
  x       := normalize(samples)    // zero-mean/unit-variance (wav2vec2 feature extractor)
  logits  := session.Run(x)        // ONNX Runtime
  return softmax(logits) → []classify.LabelScore   // emotion labels
```

`main.go` registers it via `sdk.WithClassifier("onnx-ser", backend)` and the
`audio_emotion` eval in `caller.pack.json` scores the caller's speech.

## Prerequisites

- A **C compiler** (`cgo`) — needed to build the `onnxruntime_go` binding.
  Present by default on macOS (Xcode CLT) and most Linux dev setups.
- `curl` and `tar` for the one-time model/runtime download.

## Setup & run

```bash
# One-time: download libonnxruntime + the ONNX model into ./lib and ./models
# (both gitignored). ~120 MB total; no HF token required.
make setup

# Run the demo
make run
```

Expected output:

```
Scoring caller audio: testdata/sample-16k-mono.wav
Done.
  [audio_emotion] angry score 0.052 (score=0.052)
```

The shipped `testdata/sample-16k-mono.wav` is a short **synthetic** tone, so
the emotion score is not semantically meaningful — it exists so the demo runs
out of the box. Point `AUDIO_FILE` at a real 16 kHz mono recording for a
meaningful score.

## Configuration (env overrides)

| Variable | Default | Purpose |
|----------|---------|---------|
| `AUDIO_FILE` | `testdata/sample-16k-mono.wav` | Audio to score (16 kHz mono PCM16 WAV) |
| `ONNX_MODEL` | `models/model.onnx` | Path to the `.onnx` model |
| `ONNX_LIB` | `lib/libonnxruntime.{dylib,so}` | Path to the ONNX Runtime shared library |
| `MODEL_FILE` | `model_quantized.onnx` | Which model variant `make setup` fetches (set to `model.onnx` for full precision) |
| `ORT_VERSION` | `1.22.0` | ONNX Runtime release to download |

## Swapping models

The default model outputs six emotions in this index order:
`[sad, angry, disgust, fear, happy, neutral]` (from its `config.json`
`id2label`). To use a different model, update `defaultEmotionLabels` **and**
the input/output tensor names (`input_values` / `logits`) in `onnx_backend.go`
to match the new model's graph, and set `MODEL_URL` for `make setup`.

## Notes

- `input_values` / `logits` are the standard tensor names for an `optimum`-
  exported `Wav2Vec2ForSequenceClassification` model.
- `onnxruntime_go` and the ONNX Runtime shared library must expose compatible
  API versions — this example pins `onnxruntime_go v1.20.0` with ORT `1.22.0`.
