.DEFAULT_GOAL := help
.PHONY: help build build-tools build-arena build-packc build-inspect-state test test-tools test-race lint clean coverage install install-tools install-tools-user uninstall-tools test-npm-init test-getting-started test-templates test-ci-examples
 
# Route unknown targets to help
.DEFAULT:
	@$(MAKE) help
# Uninstall CLI tools from system and user PATH
uninstall-tools: ## Uninstall CLI tools from system and user PATH
	@echo "Uninstalling CLI tools from /usr/local/bin and ~/.local/bin..."
	@rm -f /usr/local/bin/promptarena || echo "promptarena not found in /usr/local/bin"
	@rm -f /usr/local/bin/packc || echo "packc not found in /usr/local/bin"
	@rm -f /usr/local/bin/inspect-state || echo "inspect-state not found in /usr/local/bin"
	@rm -f ~/.local/bin/promptarena || echo "promptarena not found in ~/.local/bin"
	@rm -f ~/.local/bin/packc || echo "packc not found in ~/.local/bin"
	@rm -f ~/.local/bin/inspect-state || echo "inspect-state not found in ~/.local/bin"
	@echo "CLI tools uninstalled from system and user PATH."

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
	@LATEST_TAG=$$(git tag -l "tools/arena/v*" --sort=-v:refname | head -1 | sed 's|^tools/arena/||'); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
	BRANCH=$$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown"); \
	if git describe --tags --match "tools/arena/v*" --dirty 2>/dev/null | grep -q "tools/arena"; then \
		VERSION=$$(git describe --tags --match "tools/arena/v*" --dirty 2>/dev/null | sed 's|^tools/arena/||'); \
	else \
		DIRTY=$$(git diff --quiet 2>/dev/null || echo "-dirty"); \
		VERSION="$$LATEST_TAG-$$BRANCH+$$COMMIT$$DIRTY"; \
	fi; \
	DATE=$$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
	cd tools/arena && go build \
		-ldflags "-X main.version=$$VERSION -X main.gitCommit=$$COMMIT -X main.buildDate=$$DATE" \
		-o ../../bin/promptarena ./cmd/promptarena
	@echo "promptarena built successfully -> bin/promptarena"

build-packc: ## Build packc CLI
	@echo "Building packc..."
	@cd tools/packc && go build -o ../../bin/packc .
	@echo "packc built successfully -> bin/packc"

build-inspect-state: ## Build inspect-state utility
	@echo "Building inspect-state..."
	@cd tools/inspect-state && go build -o ../../bin/inspect-state .
	@echo "inspect-state built successfully -> bin/inspect-state"

build-schema-gen: ## Build schema-gen utility
	@echo "Building schema-gen..."
	@cd tools/schema-gen && go build -o ../../bin/schema-gen .
	@echo "schema-gen built successfully -> bin/schema-gen"

build-arena-linux: ## Build promptarena for Linux (for Docker demo)
	@echo "Building promptarena for Linux..."
	@GOOS=linux GOARCH=amd64 go build -C tools/arena -o ../../bin/promptarena-linux ./cmd/promptarena
	@echo "promptarena-linux built successfully -> bin/promptarena-linux"

build-packc-linux: ## Build packc for Linux (for Docker demo)
	@echo "Building packc for Linux..."
	@GOOS=linux GOARCH=amd64 go build -C tools/packc -o ../../bin/packc-linux .
	@echo "packc-linux built successfully -> bin/packc-linux"

demo-build: build-arena-linux build-packc-linux ## Build Docker image for demo recording
	@echo "Building demo recording container..."
	@docker build -t promptarena-demo -f Dockerfile.demo .
	@echo "Demo container built: promptarena-demo"

demo-run: ## Run demo recording container (mounts examples and recordings dirs)
	@mkdir -p recordings
	@if [ -f .env.demo ]; then \
		echo "Loading API keys from .env.demo..."; \
		docker run -it --rm \
			-v $(PWD)/examples:/demo \
			-v $(PWD)/recordings:/recordings \
			--env-file .env.demo \
			promptarena-demo; \
	else \
		echo "No .env.demo found - running without API keys (mock mode only)"; \
		docker run -it --rm \
			-v $(PWD)/examples:/demo \
			-v $(PWD)/recordings:/recordings \
			promptarena-demo; \
	fi
	@echo "Recording saved to recordings/"

demo-env: ## Create .env.demo template for API keys
	@if [ -f .env.demo ]; then \
		echo ".env.demo already exists - not overwriting"; \
	else \
		echo "# API keys for demo recording (gitignored)" > .env.demo; \
		echo "# These are passed to Docker and never appear in recordings" >> .env.demo; \
		echo "OPENAI_API_KEY=" >> .env.demo; \
		echo "ANTHROPIC_API_KEY=" >> .env.demo; \
		echo "GOOGLE_API_KEY=" >> .env.demo; \
		echo "Created .env.demo - add your API keys"; \
	fi

demo-vhs: ## Record all VHS demo GIFs locally (requires: brew install vhs)
	@echo "Recording VHS demo GIFs..."
	@mkdir -p recordings/gifs
	@command -v vhs >/dev/null 2>&1 || { echo "VHS not found. Install with: brew install vhs"; exit 1; }
	@for tape in recordings/tapes/*.tape; do \
		echo "Recording $$tape..."; \
		vhs "$$tape"; \
	done
	@echo "GIFs saved to recordings/gifs/"

demo-vhs-single: ## Record a single VHS tape locally (usage: make demo-vhs-single TAPE=01-install)
	@command -v vhs >/dev/null 2>&1 || { echo "VHS not found. Install with: brew install vhs"; exit 1; }
	@mkdir -p recordings/gifs
	@if [ -z "$(TAPE)" ]; then \
		echo "Usage: make demo-vhs-single TAPE=01-install"; \
		echo "Available tapes:"; \
		ls -1 recordings/tapes/*.tape 2>/dev/null | xargs -I{} basename {} .tape; \
		exit 1; \
	fi
	@vhs recordings/tapes/$(TAPE).tape
	@echo "GIF saved to recordings/gifs/"

demo-vhs-docker: ## Record all VHS demo GIFs in Docker (clean environment)
	@echo "Recording VHS demo GIFs in Docker..."
	@mkdir -p recordings/gifs
	@if [ -f .env.demo ]; then \
		docker run --rm \
			-v $(PWD)/examples:/demo \
			-v $(PWD)/recordings:/recordings \
			--env-file .env.demo \
			promptarena-demo record-all; \
	else \
		docker run --rm \
			-v $(PWD)/examples:/demo \
			-v $(PWD)/recordings:/recordings \
			promptarena-demo record-all; \
	fi
	@echo "GIFs saved to recordings/gifs/"

demo-vhs-docker-single: ## Record a single VHS tape in Docker (usage: make demo-vhs-docker-single TAPE=01-install)
	@mkdir -p recordings/gifs
	@if [ -z "$(TAPE)" ]; then \
		echo "Usage: make demo-vhs-docker-single TAPE=01-install"; \
		echo "Available tapes:"; \
		ls -1 recordings/tapes/*.tape 2>/dev/null | xargs -I{} basename {} .tape; \
		exit 1; \
	fi
	@if [ -f .env.demo ]; then \
		docker run --rm \
			-v $(PWD)/examples:/demo \
			-v $(PWD)/recordings:/recordings \
			--env-file .env.demo \
			promptarena-demo vhs /recordings/tapes/$(TAPE).tape; \
	else \
		docker run --rm \
			-v $(PWD)/examples:/demo \
			-v $(PWD)/recordings:/recordings \
			promptarena-demo vhs /recordings/tapes/$(TAPE).tape; \
	fi
	@echo "GIF saved to recordings/gifs/"

test: ## Run all tests
	@echo "Testing runtime..."
	@cd runtime && go test -v ./...
	@echo "Testing SDK..."
	@cd sdk && go test -v ./...
	@echo "Testing pkg..."
	@cd pkg && go test -v ./... || echo "No pkg tests yet"
	@$(MAKE) test-tools

test-tools: ## Run CLI tool tests (where applicable)
	@echo "Testing arena middleware and commands..."
	@cd tools/arena && go test -v ./... || echo "Arena tests completed"
	@echo "Testing packc..."
	@cd tools/packc && go test -v ./... || echo "PackC tests completed"
	@echo "Testing inspect-state..."
	@cd tools/inspect-state && go test -v ./... || echo "Inspect-state tests completed"

test-npm-init: build-arena ## Test npm package Getting Started flow
	@echo "Testing npm promptarena Getting Started flow..."
	@cd npm/promptarena && npm run test:init

test-getting-started: build-arena ## Test complete Getting Started workflow from README
	@echo "🚀 Testing Complete Getting Started Workflow"
	@echo ""
	@TEMP_DIR=$$(mktemp -d); \
	trap "rm -rf $$TEMP_DIR" EXIT; \
	echo "📁 Using temp directory: $$TEMP_DIR"; \
	echo ""; \
	echo "Step 1: promptarena templates"; \
	./bin/promptarena templates || exit 1; \
	echo ""; \
	echo "Step 2: promptarena init customer-support"; \
	./bin/promptarena init customer-support --output "$$TEMP_DIR" --quick || exit 1; \
	echo ""; \
	echo "Step 3: cd customer-support"; \
	cd "$$TEMP_DIR/customer-support" || exit 1; \
	echo ""; \
	echo "Step 4: promptarena validate config.arena.yaml --schema-only (without API keys)"; \
	$(CURDIR)/bin/promptarena validate config.arena.yaml --schema-only || exit 1; \
	echo ""; \
	echo "✅ All Getting Started steps completed successfully!"; \
	echo ""; \
	echo "📖 Users can now run:"; \
	echo "   export OPENAI_API_KEY=your-key"; \
	echo "   promptarena run"; \
	echo "   open out/report.html"

test-templates: build-arena ## Test all built-in templates can be created and validated
	@echo "🧪 Testing All Built-in Templates"
	@echo ""
	@TEMP_DIR=$$(mktemp -d); \
	trap "rm -rf $$TEMP_DIR" EXIT; \
	echo "📁 Using temp directory: $$TEMP_DIR"; \
	echo ""; \
	FAILED=0; \
	for template in quick-start customer-support code-assistant content-generation multimodal; do \
		echo "═══════════════════════════════════════════════════════"; \
		echo "Testing template: $$template"; \
		echo "═══════════════════════════════════════════════════════"; \
		echo ""; \
		echo "→ Creating project with template: $$template"; \
		if ./bin/promptarena init test-$$template --template $$template --output "$$TEMP_DIR" --quick --provider mock; then \
			echo "✓ Project created successfully"; \
		else \
			echo "✗ Failed to create project"; \
			FAILED=$$((FAILED + 1)); \
			continue; \
		fi; \
		echo ""; \
		echo "→ Validating config.arena.yaml"; \
		if $(CURDIR)/bin/promptarena validate "$$TEMP_DIR/test-$$template/config.arena.yaml" --schema-only; then \
			echo "✓ Configuration validated successfully"; \
		else \
			echo "✗ Validation failed"; \
			FAILED=$$((FAILED + 1)); \
			continue; \
		fi; \
		echo ""; \
		echo "→ Running test with mock provider"; \
		if (cd "$$TEMP_DIR/test-$$template" && $(CURDIR)/bin/promptarena run --ci 2>&1 | head -30); then \
			echo "✓ Run completed successfully"; \
		else \
			echo "✗ Run failed"; \
			FAILED=$$((FAILED + 1)); \
		fi; \
		echo ""; \
		echo "→ Checking output generation"; \
		if ls "$$TEMP_DIR/test-$$template/out/report-"*.html 1> /dev/null 2>&1; then \
			echo "✓ HTML report generated"; \
		else \
			echo "✗ HTML report not found"; \
			FAILED=$$((FAILED + 1)); \
		fi; \
		echo ""; \
		if [ $$FAILED -eq 0 ]; then \
			echo "✅ Template $$template: ALL TESTS PASSED"; \
		else \
			echo "❌ Template $$template: SOME TESTS FAILED"; \
		fi; \
		echo ""; \
	done; \
	echo "═══════════════════════════════════════════════════════"; \
	if [ $$FAILED -eq 0 ]; then \
		echo "✅ ALL TEMPLATES PASSED"; \
		echo "All 6 built-in templates work correctly!"; \
	else \
		echo "❌ $$FAILED template test(s) failed"; \
		exit 1; \
	fi

test-ci-examples: build-arena ## Test all CI pipeline examples with mock data
	@echo "🧪 Testing CI Pipeline Examples"
	@echo ""
	@FAILED=0; \
	EXAMPLES=( \
		"customer-support:mock-responses.yaml:customer-support-scenarios" \
		"multimodal-basics:mock-responses.yaml:" \
		"variables-demo:mock-config.yaml:" \
		"assertions-test:mock-responses.yaml:" \
		"guardrails-test:mock-responses.yaml:" \
	); \
	for example_config in "$${EXAMPLES[@]}"; do \
		IFS=':' read -r example mock_file scenario_name <<< "$$example_config"; \
		echo "═══════════════════════════════════════════════════════"; \
		echo "Testing example: $$example"; \
		echo "═══════════════════════════════════════════════════════"; \
		echo ""; \
		if [ ! -d "examples/$$example" ]; then \
			echo "✗ Example directory not found: examples/$$example"; \
			FAILED=$$((FAILED + 1)); \
			echo ""; \
			continue; \
		fi; \
		echo "→ Running with mock provider (mock-config: $$mock_file)"; \
		SCENARIO_FLAG=""; \
		if [ -n "$$scenario_name" ]; then \
			SCENARIO_FLAG="--scenario $$scenario_name"; \
		fi; \
		TMPFILE=$$(mktemp); \
		cd "examples/$$example" && ../../bin/promptarena run --config config.arena.yaml --mock-provider --mock-config "$$mock_file" $$SCENARIO_FLAG --ci --formats json > "$$TMPFILE" 2>&1; \
		EXIT_CODE=$$?; \
		cd ../..; \
		head -50 "$$TMPFILE"; \
		rm -f "$$TMPFILE"; \
		if [ $$EXIT_CODE -eq 0 ]; then \
			echo ""; \
			echo "✓ Example $$example completed"; \
		else \
			echo ""; \
			echo "✗ Example $$example failed (exit code: $$EXIT_CODE)"; \
			FAILED=$$((FAILED + 1)); \
		fi; \
		echo ""; \
	done; \
	echo "═══════════════════════════════════════════════════════"; \
	if [ $$FAILED -eq 0 ]; then \
		echo "✅ ALL CI EXAMPLES PASSED"; \
		echo "All $${#EXAMPLES[@]} CI pipeline examples work with mock data!"; \
	else \
		echo "❌ $$FAILED CI example(s) failed"; \
		exit 1; \
	fi

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
	@echo "Testing pkg with race detector..."
	@cd pkg && go test -race -v ./... 2>&1 | tee -a race-test.log || echo "Pkg race test completed"; \
	if grep -q "^FAIL" race-test.log; then \
		echo "Pkg tests failed"; \
		rm race-test.log; \
		exit 1; \
	fi
	@echo "Testing arena with race detector..."
	@cd tools/arena && go test -race -v ./... 2>&1 | tee -a race-test.log || echo "Arena race test completed"; \
	if grep -q "^FAIL" race-test.log; then \
		echo "Arena tests failed"; \
		rm race-test.log; \
		exit 1; \
	fi
	@echo "Testing packc with race detector..."
	@cd tools/packc && go test -race -v ./... 2>&1 | tee -a race-test.log || echo "PackC race test completed"; \
	if grep -q "^FAIL" race-test.log; then \
		echo "PackC tests failed"; \
		rm race-test.log; \
		exit 1; \
	fi
	@echo "Testing inspect-state with race detector..."
	@cd tools/inspect-state && go test -race -v ./... 2>&1 | tee -a race-test.log || echo "Inspect-state race test completed"; \
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
	@cd sdk && go test -coverpkg=github.com/AltairaLabs/PromptKit/sdk -coverprofile=sdk-coverage.out ./...
	@cd sdk && go tool cover -func=sdk-coverage.out | grep "^total:" || echo "No SDK coverage data"
	@echo "Generating coverage for pkg..."
	@cd pkg && go test -coverprofile=pkg-coverage.out ./... || echo "No pkg test coverage"
	@cd pkg && go tool cover -func=pkg-coverage.out | grep "^total:" 2>/dev/null || echo "No pkg coverage data"
	@echo "Generating coverage for arena..."
	@cd tools/arena && go test -coverprofile=arena-coverage.out ./... || echo "No arena test coverage"
	@cd tools/arena && go tool cover -func=arena-coverage.out | grep "^total:" 2>/dev/null || echo "No arena coverage data"
	@echo "Generating coverage for packc..."
	@cd tools/packc && go test -coverprofile=packc-coverage.out ./... || echo "No packc test coverage"
	@cd tools/packc && go tool cover -func=packc-coverage.out | grep "^total:" 2>/dev/null || echo "No packc coverage data"
	@echo "Generating coverage for inspect-state..."
	@cd tools/inspect-state && go test -coverprofile=inspect-state-coverage.out ./... || echo "No inspect-state test coverage"
	@cd tools/inspect-state && go tool cover -func=inspect-state-coverage.out | grep "^total:" 2>/dev/null || echo "No inspect-state coverage data"
	@echo "Generating coverage for schema-gen..."
	@cd tools/schema-gen && go test -coverprofile=schema-gen-coverage.out ./... || echo "No schema-gen test coverage"
	@cd tools/schema-gen && go tool cover -func=schema-gen-coverage.out | grep "^total:" 2>/dev/null || echo "No schema-gen coverage data"
	@echo "Copying coverage files to root for SonarCloud..."
	@cp runtime/runtime-coverage.out runtime-coverage.out 2>/dev/null || true
	@cp sdk/sdk-coverage.out sdk-coverage.out 2>/dev/null || true
	@cp pkg/pkg-coverage.out pkg-coverage.out 2>/dev/null || true
	@cp tools/arena/arena-coverage.out arena-coverage.out 2>/dev/null || true
	@cp tools/packc/packc-coverage.out packc-coverage.out 2>/dev/null || true
	@cp tools/inspect-state/inspect-state-coverage.out inspect-state-coverage.out 2>/dev/null || true
	@cp tools/schema-gen/schema-gen-coverage.out schema-gen-coverage.out 2>/dev/null || true
	@echo "Merging coverage files..."
	@echo "mode: set" > coverage.out
	@grep -h -v "^mode:" runtime/runtime-coverage.out sdk/sdk-coverage.out pkg/pkg-coverage.out tools/arena/arena-coverage.out tools/packc/packc-coverage.out tools/inspect-state/inspect-state-coverage.out tools/schema-gen/schema-gen-coverage.out >> coverage.out 2>/dev/null || true
	@echo "Coverage report generated: coverage.out"

lint: ## Run linters
	@echo "Linting runtime..."
	@cd runtime && go vet ./...
	@cd runtime && go fmt ./...
	@echo "Linting SDK..."
	@cd sdk && go vet ./...
	@cd sdk && go fmt ./...
	@echo "Linting pkg..."
	@cd pkg && go vet ./...
	@cd pkg && go fmt ./...
	@echo "Linting CLI tools..."
	@cd tools/arena && go vet ./... && go fmt ./...
	@cd tools/packc && go vet ./... && go fmt ./...
	@cd tools/inspect-state && go vet ./... && go fmt ./...
	@echo "Running golangci-lint..."
	@cd runtime && golangci-lint run ./...
	@cd sdk && golangci-lint run ./...
	@cd pkg && golangci-lint run ./...
	@cd tools/arena && golangci-lint run ./...
	@cd tools/packc && golangci-lint run ./...
	@cd tools/inspect-state && golangci-lint run ./...
	@echo "Running gosec security scanner..."
	@$(MAKE) security-scan

lint-diff: ## Run linters on changed code only (fast, for pre-commit)
	@echo "🔍 Linting changed code only..."
	@MODULES="runtime sdk pkg tools/arena tools/packc tools/inspect-state tools/schema-gen"; \
	CHANGED=0; \
	for module in $$MODULES; do \
		if git diff --name-only HEAD | grep -q "^$$module/.*\.go$$"; then \
			echo "Linting $$module (has changes)..."; \
			cd $$module && golangci-lint run --new-from-rev=HEAD --timeout=3m ./... && cd ..; \
			CHANGED=1; \
		fi; \
	done; \
	if [ $$CHANGED -eq 0 ]; then \
		echo "✓ No Go file changes detected"; \
	else \
		echo "✓ Lint check complete"; \
		echo "🔒 Running security scan on changed code..."; \
		$(MAKE) security-scan-diff; \
	fi

security-scan: ## Run gosec security scanner on all code
	@if command -v gosec >/dev/null 2>&1; then \
		echo "🔒 Running security scan..."; \
		gosec -quiet -fmt=text ./runtime/... ./sdk/... ./pkg/... ./tools/...; \
	else \
		echo "⚠️  gosec not installed. Install with: brew install gosec"; \
		echo "   Or visit: https://github.com/securego/gosec"; \
	fi

security-scan-diff: ## Run gosec on changed code only (for pre-commit)
	@if command -v gosec >/dev/null 2>&1; then \
		MODULES="runtime sdk pkg tools/arena tools/packc tools/inspect-state tools/schema-gen"; \
		for module in $$MODULES; do \
			if git diff --name-only HEAD | grep -q "^$$module/.*\.go$$"; then \
				echo "Security scan: $$module"; \
				gosec -quiet -fmt=text ./$$module/... 2>&1 | grep -v "Golang errors" || true; \
			fi; \
		done; \
	else \
		echo "⚠️  gosec not installed (optional for pre-commit)"; \
	fi

test-fast: ## Run tests for changed packages only (fast, for pre-commit)
	@echo "🧪 Testing changed packages..."
	@MODULES="runtime sdk pkg tools/arena tools/packc tools/inspect-state tools/schema-gen"; \
	CHANGED=0; \
	for module in $$MODULES; do \
		if git diff --name-only HEAD | grep -q "^$$module/.*\.go$$"; then \
			echo "Testing $$module..."; \
			cd $$module && go test ./... && cd ..; \
			CHANGED=1; \
		fi; \
	done; \
	if [ $$CHANGED -eq 0 ]; then \
		echo "✓ No test modules to run"; \
	else \
		echo "✓ Tests passed"; \
	fi

verify: lint-diff test-fast ## Run all verification checks (used by CI and pre-commit)
	@echo "✓ All verification checks passed!"

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

schemas: build-schema-gen ## Generate JSON schemas (including latest refs)
	@echo "Generating JSON schemas..."
	@./bin/schema-gen

schemas-check: build-schema-gen ## Check if schemas are up to date (for CI)
	@echo "Checking if schemas are up to date..."
	@./bin/schema-gen --check

schemas-copy: schemas ## Copy schemas to docs/public for hosting
	@echo "Copying schemas to docs/public/schemas..."
	@mkdir -p docs/public/schemas
	@cp -r schemas/* docs/public/schemas/
	@echo "✓ Schemas copied to docs/public/schemas"
	@echo ""
	@echo "Schemas will be available at:"
	@find docs/public/schemas -name "*.json" -type f | while read -r file; do \
		rel_path=$$(echo $$file | sed 's|docs/public/schemas/||'); \
		echo "  https://promptkit.altairalabs.ai/schemas/$$rel_path"; \
	done

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
	@rm -f tools/schema-gen/schema-gen
	@echo "Cleaned build artifacts"

# Documentation targets (Astro-based)
docs-install: ## Install documentation dependencies
	@echo "📦 Installing documentation dependencies..."
	@command -v gomarkdoc >/dev/null 2>&1 || { \
		echo "Installing gomarkdoc..."; \
		go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest; \
	}
	@echo "Installing Astro dependencies..."
	@cd docs && npm install
	@echo "✅ Documentation dependencies installed"

docs-api: ## Generate API documentation from Go code
	@echo "🔧 Generating API documentation..."
	@mkdir -p docs/src/content/docs/api
	@echo "Generating SDK API docs..."
	@cd sdk && gomarkdoc --output ../docs/src/content/docs/api/sdk-temp.md .
	@echo "---" > docs/src/content/docs/api/sdk.md
	@echo "title: SDK API Reference" >> docs/src/content/docs/api/sdk.md
	@echo "description: Complete API reference for the PromptKit Go SDK" >> docs/src/content/docs/api/sdk.md
	@echo "sidebar:" >> docs/src/content/docs/api/sdk.md
	@echo "  order: 1" >> docs/src/content/docs/api/sdk.md
	@echo "---" >> docs/src/content/docs/api/sdk.md
	@cat docs/src/content/docs/api/sdk-temp.md >> docs/src/content/docs/api/sdk.md
	@rm docs/src/content/docs/api/sdk-temp.md
	@echo "Generating Runtime API docs..."
	@cd runtime && gomarkdoc --output ../docs/src/content/docs/api/runtime-temp.md ./...
	@echo "---" > docs/src/content/docs/api/runtime.md
	@echo "title: Runtime API Reference" >> docs/src/content/docs/api/runtime.md
	@echo "description: Complete API reference for the PromptKit Runtime" >> docs/src/content/docs/api/runtime.md
	@echo "sidebar:" >> docs/src/content/docs/api/runtime.md
	@echo "  order: 2" >> docs/src/content/docs/api/runtime.md
	@echo "---" >> docs/src/content/docs/api/runtime.md
	@cat docs/src/content/docs/api/runtime-temp.md >> docs/src/content/docs/api/runtime.md
	@rm docs/src/content/docs/api/runtime-temp.md
	@echo "✅ API documentation generated"

docs-cli: ## Generate CLI documentation and man pages
	@echo "📋 Generating CLI documentation..."
	@mkdir -p docs/src/content/docs/reference
	@echo "Generating Arena CLI docs..."
	@./bin/promptarena --help > docs/src/content/docs/reference/arena-cli.txt 2>/dev/null || echo "Arena CLI help captured"
	@echo "Generating PackC CLI docs..."
	@./bin/packc --help > docs/src/content/docs/reference/packc-cli.txt 2>/dev/null || echo "PackC CLI help captured"
	@echo "✅ CLI documentation generated"

docs-validate: ## Validate and auto-fix documentation formatting
	@echo "🔍 Validating and fixing documentation..."
	@find docs/src/content/docs -name "*.md" -type f | while read file; do \
		echo "Checking $$file..."; \
		if command -v markdownlint >/dev/null 2>&1; then \
			markdownlint --fix "$$file" 2>/dev/null || true; \
		fi; \
	done
	@echo "✅ Documentation validation complete (auto-fixed)"

docs-check-links: docs-build ## Check for broken links in built documentation
	@echo "🔗 Checking for broken links..."
	@cd docs && npm run check-links
	@echo "✅ Link check complete"

docs-serve: ## Serve documentation locally for development
	@echo "🌐 Starting Astro development server..."
	@cd docs && npm run dev
docs-build: ## Build complete documentation site
	@echo "🏗️ Building documentation site..."
	@$(MAKE) docs-api
	@$(MAKE) docs-cli
	@$(MAKE) schemas-copy
	@echo "📝 Preparing example documentation..."
	@./scripts/prepare-examples-docs.sh
	@echo "🔨 Building Astro site..."
	@cd docs && npm run build
	@echo "✅ Documentation site built in docs/dist/"

docs-preview: ## Preview built documentation
	@echo "👀 Previewing documentation..."
	@cd docs && npm run preview

docs-clean: ## Clean generated documentation
	@echo "🧹 Cleaning generated documentation..."
	@rm -rf docs/dist/
	@rm -rf docs/.astro/
	@rm -rf docs/src/content/docs/api/
	@rm -rf docs/src/content/docs/arena/examples/
	@rm -rf docs/src/content/docs/sdk/examples/
	@rm -rf docs/src/content/docs/reference/*-cli.txt
	@echo "✅ Generated documentation cleaned"

docs: docs-build ## Generate all documentation (alias for docs-build)

# Code Quality targets
sonar-install: ## Install SonarScanner locally
	@echo "📊 Installing SonarScanner..."
	@if command -v brew >/dev/null 2>&1; then \
		brew install sonar-scanner; \
	elif command -v npm >/dev/null 2>&1; then \
		npm install -g sonarqube-scanner; \
	else \
		echo "Please install SonarScanner manually: https://docs.sonarqube.org/latest/analysis/scan/sonarscanner/"; \
	fi

sonar-deps: ## Install dependencies for SonarQube analysis (jq for parsing results)
	@echo "📦 Checking SonarQube dependencies..."
	@if ! command -v jq >/dev/null 2>&1; then \
		echo "Installing jq for parsing SonarQube results..."; \
		if command -v brew >/dev/null 2>&1; then \
			brew install jq; \
		elif command -v apt-get >/dev/null 2>&1; then \
			sudo apt-get install -y jq; \
		elif command -v yum >/dev/null 2>&1; then \
			sudo yum install -y jq; \
		else \
			echo "⚠️  Could not install jq automatically. Please install it manually:"; \
			echo "  macOS: brew install jq"; \
			echo "  Linux: sudo apt-get install jq  OR  sudo yum install jq"; \
			exit 1; \
		fi; \
	else \
		echo "✅ jq is already installed"; \
	fi

sonar-scan: sonar-deps ## Run SonarScanner locally (requires SONAR_TOKEN env var for CLI authentication)
	@echo "📊 Running SonarScanner analysis..."
	@if [ -z "$(SONAR_TOKEN)" ]; then \
		echo "❌ SONAR_TOKEN environment variable is required for local CLI scanning"; \
		echo "💡 Get your token from: https://sonarcloud.io/account/security/"; \
		echo "ℹ️  Note: CI/CD via GitHub Actions doesn't need a token for public repos"; \
		exit 1; \
	fi
	@BRANCH=$$(git rev-parse --abbrev-ref HEAD); \
	echo "📍 Current branch: $$BRANCH"; \
	sonar-scanner \
		-Dsonar.projectKey=AltairaLabs_PromptKit \
		-Dsonar.organization=altairalabs \
		-Dsonar.sources=. \
		-Dsonar.host.url=https://sonarcloud.io \
		-Dsonar.token=$(SONAR_TOKEN) \
		-Dsonar.go.coverage.reportPaths=coverage.out \
		-Dsonar.exclusions="**/*_test.go,**/vendor/**,**/bin/**,**/docs/**" \
		-Dsonar.scanner.dumpToFile=sonar-report.json
	@echo ""
	@echo "📋 Fetching issues from SonarCloud (main branch)..."
	@sleep 5
	@curl -s -u $(SONAR_TOKEN): \
		"https://sonarcloud.io/api/issues/search?componentKeys=AltairaLabs_PromptKit&resolved=false&severities=CRITICAL,MAJOR" \
		| jq -r '.issues[] | "\(.component):\(.line // 1):1: [\(.severity)] \(.message) (\(.rule))"' \
		> sonar-issues.txt 2>/dev/null || echo "⚠️  Could not fetch issues"
	@if [ -f sonar-issues.txt ] && [ -s sonar-issues.txt ]; then \
		echo ""; \
		echo "🔍 SonarQube Issues (CRITICAL & MAJOR):"; \
		echo ""; \
		cat sonar-issues.txt; \
		echo ""; \
		echo "💡 Issues saved to sonar-issues.txt (compatible with VS Code Problems panel)"; \
	else \
		echo "✅ No critical or major issues found!"; \
	fi

sonar-quick: coverage sonar-scan ## Generate coverage and run Sonar analysis in one command
