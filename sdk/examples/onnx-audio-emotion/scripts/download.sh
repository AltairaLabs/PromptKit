#!/usr/bin/env bash
# Downloads the ONNX Runtime shared library and the wav2vec2 speech-emotion
# model into gitignored ./lib and ./models. Idempotent — re-running skips
# anything already present. No Hugging Face token required (public model).
set -euo pipefail
cd "$(dirname "$0")/.."

ORT_VERSION="${ORT_VERSION:-1.20.1}"
MODEL_REPO="onnx-community/wav2vec2-base-Speech_Emotion_Recognition-ONNX"
MODEL_FILE="${MODEL_FILE:-model_quantized.onnx}" # 95 MB int8; set to model.onnx for full precision
MODEL_URL="${MODEL_URL:-https://huggingface.co/${MODEL_REPO}/resolve/main/onnx/${MODEL_FILE}}"

mkdir -p lib models

# 1. libonnxruntime for this platform.
if [ ! -e lib/libonnxruntime.dylib ] && [ ! -e lib/libonnxruntime.so ]; then
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os-$arch" in
    Darwin-arm64) pkg="onnxruntime-osx-arm64-${ORT_VERSION}" ;;
    Darwin-x86_64) pkg="onnxruntime-osx-x86_64-${ORT_VERSION}" ;;
    Linux-x86_64) pkg="onnxruntime-linux-x64-${ORT_VERSION}" ;;
    Linux-aarch64) pkg="onnxruntime-linux-aarch64-${ORT_VERSION}" ;;
    *) echo "unsupported platform $os-$arch; download libonnxruntime manually into ./lib" >&2; exit 1 ;;
  esac
  url="https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/${pkg}.tgz"
  echo "Fetching ONNX Runtime: $url"
  curl -fsSL "$url" -o /tmp/ort.tgz
  tar -xzf /tmp/ort.tgz -C /tmp
  cp /tmp/"${pkg}"/lib/libonnxruntime.* lib/
  echo "Installed ONNX Runtime into ./lib"
fi

# 2. The speech-emotion model.
if [ ! -e models/model.onnx ]; then
  echo "Fetching model: $MODEL_URL"
  curl -fsSL "$MODEL_URL" -o models/model.onnx
  echo "Installed model into ./models/model.onnx"
fi

echo "Setup complete. ./lib and ./models are populated (both gitignored)."
