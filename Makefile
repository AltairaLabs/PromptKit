.PHONY: help build test lint clean arena sdk all coverage install

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: build test lint ## Build and test everything

build: ## Build all components
	@echo "Building Arena..."
	@cd tools/arena && go build -o ../../bin/promptarena ./cmd/promptarena
	@echo "Building Pack Compiler..."
	@cd tools/packc && go build -o ../../bin/packc .
	@echo "Building SDK..."
	@cd sdk && go build ./...
	@echo "Building runtime..."
	@cd runtime && go build ./...

arena: ## Build just the Arena tool
	@cd tools/arena && go build -o ../../bin/promptarena ./cmd/promptarena
	@echo "Arena built: bin/promptarena"

packc: ## Build just the Pack Compiler
	@cd tools/packc && go build -o ../../bin/packc .
	@echo "Pack Compiler built: bin/packc"

sdk: ## Build just the SDK
	@cd sdk && go build ./...
	@echo "SDK built successfully"

test: ## Run all tests
	@echo "Testing Arena..."
	@cd tools/arena && go test -v ./...
	@echo "Testing SDK..."
	@cd sdk && go test -v ./...
	@echo "Testing runtime..."
	@cd runtime && go test -v ./...

test-race: ## Run tests with race detector
	@cd tools/arena && go test -race ./...
	@cd sdk && go test -race ./...
	@cd runtime && go test -race ./...

coverage: ## Generate test coverage report
	@echo "Generating coverage for Arena..."
	@cd tools/arena && go test -coverprofile=coverage.out ./...
	@cd tools/arena && go tool cover -func=coverage.out | grep "^total:"
	@echo "Generating coverage for SDK..."
	@cd sdk && go test -coverprofile=coverage.out ./...
	@cd sdk && go tool cover -func=coverage.out | grep "^total:"
	@echo "Generating coverage for runtime..."
	@cd runtime && go test -coverprofile=coverage.out ./...
	@cd runtime && go tool cover -func=coverage.out | grep "^total:"

lint: ## Run linters
	@echo "Linting Arena..."
	@cd tools/arena && golangci-lint run ./... || true
	@echo "Linting SDK..."
	@cd sdk && golangci-lint run ./... || true
	@echo "Linting runtime..."
	@cd runtime && golangci-lint run ./... || true

install: ## Install Arena CLI
	@cd tools/arena && go install ./cmd/promptarena

clean: ## Clean build artifacts
	@rm -rf bin/
	@rm -f tools/arena/coverage.out tools/arena/coverage.html
	@rm -f sdk/coverage.out sdk/coverage.html
	@rm -f runtime/coverage.out runtime/coverage.html
	@echo "Cleaned build artifacts"

fmt: ## Format all Go code
	@go fmt ./...

vet: ## Run go vet
	@cd tools/arena && go vet ./...
	@cd sdk && go vet ./...
	@cd runtime && go vet ./...

tidy: ## Tidy all go.mod files
	@cd tools/arena && go mod tidy
	@cd sdk && go mod tidy
	@cd runtime && go mod tidy

docs: ## Generate API documentation
	@echo "📚 Generating API documentation..."
	@./scripts/generate-docs.sh

docs-serve: ## Start godoc server
	@echo "🌐 Starting godoc server at http://localhost:6060"
	@godoc -http=:6060

docs-clean: ## Clean generated documentation
	@rm -rf docs/api/
	@echo "Cleaned generated docs"
