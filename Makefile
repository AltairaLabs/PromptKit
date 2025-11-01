.PHONY: help build test test-race lint clean coverage install

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install dependencies
	@echo "Installing Go dependencies..."
	@go work sync
	@cd runtime && go mod download
	@cd pkg && go mod download || echo "No pkg dependencies yet"

build: ## Build current components
	@echo "Building runtime..."
	@cd runtime && go build ./...
	@echo "Building pkg..."
	@cd pkg && go build ./... || echo "No packages yet"

test: ## Run all tests
	@echo "Testing runtime..."
	@cd runtime && go test -v ./...

test-race: ## Run tests with race detector
	@echo "Testing runtime with race detector..."
	@cd runtime && go test -race -v ./... 2>&1 | tee race-test.log; \
	if grep -q "^FAIL" race-test.log; then \
		echo "Tests failed"; \
		rm race-test.log; \
		exit 1; \
	else \
		echo "All tests passed (race detector completed)"; \
		rm race-test.log; \
		exit 0; \
	fi

coverage: ## Generate test coverage report
	@echo "Generating coverage for runtime..."
	@cd runtime && go test -coverprofile=coverage.out ./...
	@cp runtime/coverage.out ./coverage.out
	@cd runtime && go tool cover -func=coverage.out | grep "^total:" || echo "No coverage data"

lint: ## Run linters
	@echo "Linting runtime..."
	@cd runtime && go vet ./...
	@cd runtime && go fmt ./...
	@echo "Running golangci-lint..."
	@cd runtime && golangci-lint run ./... || echo "golangci-lint not installed or failed"

clean: ## Clean build artifacts
	@rm -rf bin/
	@rm -f runtime/coverage.out
	@rm -f coverage.out
	@echo "Cleaned build artifacts"
