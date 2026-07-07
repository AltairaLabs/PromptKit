.DEFAULT_GOAL := help
.PHONY: help build test test-race lint clean coverage install test-e2e test-e2e-mock test-e2e-coverage test-e2e-ci schemas schemas-check schemas-copy

# Route unknown targets to help
.DEFAULT:
	@$(MAKE) help

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
	@cd server/a2a && go mod download

build: ## Build current components
	@echo "Building runtime..."
	@cd runtime && go build ./...
	@echo "Building server/a2a..."
	@cd server/a2a && go build ./...
	@echo "Building SDK..."
	@cd sdk && go build ./...
	@echo "Building pkg..."
	@cd pkg && go build ./... || echo "No packages yet"

test: ## Run all tests
	@echo "Testing runtime..."
	@cd runtime && go test -v ./...
	@echo "Testing server/a2a..."
	@cd server/a2a && go test -v ./...
	@echo "Testing SDK..."
	@cd sdk && go test -v ./...
	@echo "Testing pkg..."
	@cd pkg && go test -v ./... || echo "No pkg tests yet"

# =============================================================================
# SDK E2E Tests
# =============================================================================

test-e2e: ## Run SDK e2e tests with available providers
	@echo "🧪 Running SDK E2E Tests..."
	@./scripts/run-e2e-tests.sh --verbose

test-e2e-mock: ## Run SDK e2e tests with mock provider only (no API keys needed)
	@echo "🧪 Running SDK E2E Tests (mock only)..."
	@./scripts/run-e2e-tests.sh --mock-only --verbose

test-e2e-coverage: ## Run SDK e2e tests with coverage and HTML report
	@echo "🧪 Running SDK E2E Tests with coverage..."
	@./scripts/run-e2e-tests.sh --coverage --html --verbose
	@echo ""
	@echo "📊 Coverage report: sdk/e2e-results/coverage.html"

test-e2e-ci: ## Run SDK e2e tests for CI (JSON + JUnit output)
	@echo "🧪 Running SDK E2E Tests (CI mode)..."
	@./scripts/run-e2e-tests.sh --coverage --json --junit
	@echo ""
	@echo "📋 Results: sdk/e2e-results/"

test-e2e-suite: ## Run specific e2e test suite (usage: make test-e2e-suite SUITE=text)
	@if [ -z "$(SUITE)" ]; then \
		echo "Usage: make test-e2e-suite SUITE=<suite>"; \
		echo "Available suites: text, vision, tools, events"; \
		exit 1; \
	fi
	@echo "🧪 Running SDK E2E Tests - Suite: $(SUITE)..."
	@./scripts/run-e2e-tests.sh --suite=$(SUITE) --verbose

test-e2e-provider: ## Run e2e tests for specific provider (usage: make test-e2e-provider PROVIDER=openai)
	@if [ -z "$(PROVIDER)" ]; then \
		echo "Usage: make test-e2e-provider PROVIDER=<provider>"; \
		echo "Available providers: openai, anthropic, gemini, mock"; \
		exit 1; \
	fi
	@echo "🧪 Running SDK E2E Tests - Provider: $(PROVIDER)..."
	@./scripts/run-e2e-tests.sh --providers=$(PROVIDER) --verbose

# =============================================================================
# Other Tests
# =============================================================================

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
	@echo "Testing server/a2a with race detector..."
	@cd server/a2a && go test -race -v ./... 2>&1 | tee -a race-test.log || echo "server/a2a race test completed"; \
	if grep -q "^FAIL" race-test.log; then \
		echo "server/a2a tests failed"; \
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
	@echo "Generating coverage for server/a2a..."
	@cd server/a2a && go test -coverprofile=server-a2a-coverage.out ./... || echo "No server/a2a test coverage"
	@cd server/a2a && go tool cover -func=server-a2a-coverage.out | grep "^total:" 2>/dev/null || echo "No server/a2a coverage data"
	@echo "Copying coverage files to root for SonarCloud..."
	@cp runtime/runtime-coverage.out runtime-coverage.out 2>/dev/null || true
	@cp sdk/sdk-coverage.out sdk-coverage.out 2>/dev/null || true
	@cp pkg/pkg-coverage.out pkg-coverage.out 2>/dev/null || true
	@cp server/a2a/server-a2a-coverage.out server-a2a-coverage.out 2>/dev/null || true
	@echo "Merging coverage files..."
	@echo "mode: set" > coverage.out
	@grep -h -v "^mode:" runtime/runtime-coverage.out sdk/sdk-coverage.out pkg/pkg-coverage.out server/a2a/server-a2a-coverage.out >> coverage.out 2>/dev/null || true
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
	@echo "Linting server/a2a..."
	@cd server/a2a && go vet ./...
	@cd server/a2a && go fmt ./...
	@echo "Running golangci-lint..."
	@cd runtime && golangci-lint run ./...
	@cd sdk && golangci-lint run ./...
	@cd pkg && golangci-lint run ./...
	@cd server/a2a && golangci-lint run ./...
	@echo "Running gosec security scanner..."
	@$(MAKE) security-scan
	@echo "Checking message content patterns..."
	@./scripts/lint-message-patterns.sh

lint-diff: ## Run linters on changed code only (fast, for pre-commit)
	@echo "🔍 Linting changed code only..."
	@MODULES="runtime sdk pkg server/a2a"; \
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
		gosec -quiet -fmt=text ./runtime/... ./sdk/... ./pkg/... ./server/...; \
	else \
		echo "⚠️  gosec not installed. Install with: brew install gosec"; \
		echo "   Or visit: https://github.com/securego/gosec"; \
	fi

security-scan-diff: ## Run gosec on changed code only (for pre-commit)
	@if command -v gosec >/dev/null 2>&1; then \
		MODULES="runtime sdk pkg server/a2a"; \
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
	@MODULES="runtime sdk pkg server/a2a"; \
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

schemas: ## Fetch JSON schemas from promptarena (the schema owner)
	@./scripts/fetch-schemas.sh

schemas-check: ## Check committed schemas match promptarena (for CI)
	@echo "Checking schemas are in sync with promptarena..."
	@tmp=$$(mktemp -d); \
	./scripts/fetch-schemas.sh "$$tmp" >/dev/null; \
	if diff -rq "$$tmp" schemas/v1alpha1 >/dev/null 2>&1; then \
		echo "✓ Schemas are in sync with promptarena"; rm -rf "$$tmp"; \
	else \
		echo "::error::schemas/v1alpha1 is out of sync with promptarena — run 'make schemas' and commit"; \
		diff -rq "$$tmp" schemas/v1alpha1 || true; rm -rf "$$tmp"; exit 1; \
	fi

schemas-copy: schemas ## Copy schemas to docs/public for hosting (+ latest refs)
	@echo "Copying schemas to docs/public/schemas..."
	@mkdir -p docs/public/schemas
	@cp -r schemas/* docs/public/schemas/
	@./scripts/gen-schema-latest-refs.sh schemas/v1alpha1 docs/public/schemas/latest
	@echo "✓ Schemas hosted at https://promptkit.altairalabs.ai/schemas/{v1alpha1,latest}/"

clean: ## Clean build artifacts
	@rm -rf bin/
	@rm -f runtime/coverage.out
	@rm -f sdk/coverage.out
	@rm -f coverage.out
	@rm -f *-coverage.out
	@rm -rf sdk/e2e-results/
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
	@rm -rf docs/src/content/docs/sdk/examples/
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
