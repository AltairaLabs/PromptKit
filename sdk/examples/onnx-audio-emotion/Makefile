.PHONY: setup run test

# One-time: download libonnxruntime + the ONNX model into ./lib and ./models.
setup:
	./scripts/download.sh

# Run the demo (requires `make setup` first).
run:
	GOWORK=off go run .

# Run the pure-Go unit tests (no native lib needed).
test:
	GOWORK=off go test ./...
