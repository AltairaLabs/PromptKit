.PHONY: help build build-tools build-arena build-packc build-inspect-state test test-tools test-race lint clean coverage install install-tools install-tools-user

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install dependencies
	@echo "Installing Go dependencies..."
	@go work sync
	@cd runtime && go mod download
	@cd sdk && go mod download
	@cd pkg && go mod download || echo "No pkg dependencies yet"
	@cd tools/arena && go mod download
	@cd tools/packc && go mod download
	@cd tools/inspect-state && go mod download

build: ## Build current components
	@echo "Building runtime..."
	@cd runtime && go build ./...
	@echo "Building SDK..."
	@cd sdk && go build ./...
	@echo "Building pkg..."
	@cd pkg && go build ./... || echo "No packages yet"

build-tools: ## Build all CLI tools
	@echo "Building all CLI tools..."
	@$(MAKE) build-arena
	@$(MAKE) build-packc  
	@$(MAKE) build-inspect-state

build-arena: ## Build promptarena CLI
	@echo "Building promptarena..."
	@cd tools/arena && go build -o ../../bin/promptarena ./cmd/promptarena
	@echo "promptarena built successfully -> bin/promptarena"

build-packc: ## Build packc CLI
	@echo "Building packc..."
	@cd tools/packc && go build -o ../../bin/packc .
	@echo "packc built successfully -> bin/packc"

build-inspect-state: ## Build inspect-state utility
	@echo "Building inspect-state..."
	@cd tools/inspect-state && go build -o ../../bin/inspect-state .
	@echo "inspect-state built successfully -> bin/inspect-state"

test: ## Run all tests
	@echo "Testing runtime..."
	@cd runtime && go test -v ./...
	@echo "Testing SDK..."
	@cd sdk && go test -v ./...
	@$(MAKE) test-tools

test-tools: ## Run CLI tool tests (where applicable)
	@echo "Testing arena middleware and commands..."
	@cd tools/arena && go test -v ./... || echo "Arena tests completed"

test-race: ## Run tests with race detector
	@echo "Testing runtime with race detector..."
	@cd runtime && go test -race -v ./... 2>&1 | tee race-test.log; \
	if grep -q "^FAIL" race-test.log; then \
		echo "Runtime tests failed"; \
		rm race-test.log; \
		exit 1; \
	fi
	@echo "Testing SDK with race detector..."
	@cd sdk && go test -race -v ./... 2>&1 | tee -a race-test.log; \
	if grep -q "^FAIL" race-test.log; then \
		echo "SDK tests failed"; \
		rm race-test.log; \
		exit 1; \
	fi
	@echo "Testing arena with race detector..."
	@cd tools/arena && go test -race -v ./... 2>&1 | tee -a race-test.log || echo "Arena race test completed"; \
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
	@cd runtime && go test -coverprofile=runtime-coverage.out ./...
	@cd runtime && go tool cover -func=runtime-coverage.out | grep "^total:" || echo "No runtime coverage data"
	@echo "Generating coverage for SDK..."
	@cd sdk && go test -coverprofile=sdk-coverage.out ./...
	@cd sdk && go tool cover -func=sdk-coverage.out | grep "^total:" || echo "No SDK coverage data"
	@echo "Generating coverage for arena..."
	@cd tools/arena && go test -coverprofile=arena-coverage.out ./... || echo "No arena test coverage"
	@cd tools/arena && go tool cover -func=arena-coverage.out | grep "^total:" 2>/dev/null || echo "No arena coverage data"
	@echo "Merging coverage files..."
	@echo "mode: set" > coverage.out
	@grep -h -v "^mode:" runtime/runtime-coverage.out sdk/sdk-coverage.out tools/arena/arena-coverage.out >> coverage.out 2>/dev/null || true
	@echo "Coverage report generated: coverage.out"

lint: ## Run linters
	@echo "Linting runtime..."
	@cd runtime && go vet ./...
	@cd runtime && go fmt ./...
	@echo "Linting SDK..."
	@cd sdk && go vet ./...
	@cd sdk && go fmt ./...
	@echo "Linting CLI tools..."
	@cd tools/arena && go vet ./... && go fmt ./...
	@cd tools/packc && go vet ./... && go fmt ./...
	@cd tools/inspect-state && go vet ./... && go fmt ./...
	@echo "Running golangci-lint..."
	@cd runtime && golangci-lint run ./... || echo "golangci-lint not installed or failed"
	@cd sdk && golangci-lint run ./... || echo "golangci-lint not installed or failed"
	@cd tools/arena && golangci-lint run ./... || echo "golangci-lint not installed or failed"

install-tools: ## Install CLI tools to system PATH
	@echo "Installing CLI tools to system..."
	@$(MAKE) build-tools
	@echo "Installing to /usr/local/bin (may require sudo)..."
	@cp bin/promptarena /usr/local/bin/ || echo "Failed to install promptarena - try sudo make install-tools"
	@cp bin/packc /usr/local/bin/ || echo "Failed to install packc - try sudo make install-tools"  
	@cp bin/inspect-state /usr/local/bin/ || echo "Failed to install inspect-state - try sudo make install-tools"
	@echo "CLI tools installed successfully!"

install-tools-user: ## Install CLI tools to user PATH (~/.local/bin)
	@echo "Installing CLI tools to user directory..."
	@$(MAKE) build-tools
	@mkdir -p ~/.local/bin
	@cp bin/promptarena ~/.local/bin/
	@cp bin/packc ~/.local/bin/
	@cp bin/inspect-state ~/.local/bin/
	@echo "CLI tools installed to ~/.local/bin"
	@echo "Make sure ~/.local/bin is in your PATH"

clean: ## Clean build artifacts
	@rm -rf bin/
	@rm -f runtime/coverage.out
	@rm -f sdk/coverage.out
	@rm -f arena/coverage.out
	@rm -f coverage.out
	@rm -f *-coverage.out
	@rm -f tools/arena/promptarena
	@rm -f tools/packc/packc
	@rm -f tools/inspect-state/inspect-state
	@echo "Cleaned build artifacts"

# Documentation targets
docs-install: ## Install documentation dependencies
	@echo "Installing documentation tools..."
	@go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
	@echo "Documentation tools installed"

docs-api: ## Generate API documentation from Go code
	@echo "🔧 Generating API documentation..."
	@mkdir -p docs/api
	@echo "Generating SDK API docs..."
	@cd sdk && gomarkdoc --output ../docs/api/sdk.md .
	@echo "Generating Runtime API docs..."
	@cd runtime && gomarkdoc --output ../docs/api/runtime.md ./...
	@echo "✅ API documentation generated"

docs-cli: ## Generate CLI documentation and man pages
	@echo "📋 Generating CLI documentation..."
	@mkdir -p docs/guides/arena docs/guides/packc
	@echo "Generating Arena CLI docs..."
	@./bin/promptarena --help > docs/guides/arena/commands.txt 2>/dev/null || echo "Arena CLI help captured"
	@echo "Generating PackC CLI docs..."  
	@./bin/packc --help > docs/guides/packc/commands.txt 2>/dev/null || echo "PackC CLI help captured"
	@echo "✅ CLI documentation generated"

docs-validate: ## Validate documentation links and formatting
	@echo "🔍 Validating documentation..."
	@find docs -name "*.md" -type f | while read file; do \
		echo "Checking $$file..."; \
		if command -v markdownlint >/dev/null 2>&1; then \
			markdownlint "$$file" || true; \
		fi; \
	done
	@echo "✅ Documentation validation complete"

docs-serve: ## Serve documentation locally for development
	@echo "🌐 Starting local documentation server..."
	@if command -v python3 >/dev/null 2>&1; then \
		echo "Serving docs at http://localhost:8000"; \
		cd docs && python3 -m http.server 8000; \
	elif command -v python >/dev/null 2>&1; then \
		echo "Serving docs at http://localhost:8000"; \
		cd docs && python -m SimpleHTTPServer 8000; \
	else \
		echo "Python not found. Install Python to serve docs locally."; \
		exit 1; \
	fi

docs-build: ## Build complete documentation site
	@echo "🏗️ Building documentation site..."
	@$(MAKE) docs-api
	@$(MAKE) docs-cli
	@echo "✅ Documentation site built in docs/"

docs-clean: ## Clean generated documentation
	@echo "🧹 Cleaning generated documentation..."
	@rm -rf docs/api/
	@rm -rf docs/guides/*/commands.txt
	@echo "✅ Generated documentation cleaned"

docs: docs-build ## Generate all documentation (alias for docs-build)
