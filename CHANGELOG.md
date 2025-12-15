# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### Stage-Based Pipeline Architecture
A complete rewrite of the runtime pipeline from middleware-based to stage-based architecture, enabling true streaming execution with concurrent processing.

**Key Changes:**
- **Stage-Based Execution**: Pipeline now uses a DAG of stages instead of middleware chain
- **True Streaming**: Elements flow through stages as they're produced via channels
- **Concurrent Processing**: Each stage runs in its own goroutine
- **Backpressure Support**: Channel-based communication naturally handles slow consumers
- **Three Pipeline Modes**: Text, VAD (Voice Activity Detection), and ASM (Audio Streaming Mode)

**New Stages:**
- Core: `StateStoreLoadStage`, `StateStoreSaveStage`, `PromptAssemblyStage`, `TemplateStage`, `ValidationStage`, `ProviderStage`
- Streaming: `VADAccumulatorStage`, `AudioTurnStage`, `STTStage`, `TTSStage`, `TTSStageWithInterruption`, `DuplexProviderStage`
- Advanced: `RouterStage`, `MergeStage`, `MetricsStage`, `TracingStage`, `PriorityChannel`
- Utility: `DebugStage`, `VariableProviderStage`, `MediaExternalizerStage`, `ContextBuilderStage`

**SDK Integration:**
- SDK now builds stage-based pipelines internally
- Three execution modes: Text (HTTP API), VAD (Audio → STT → LLM → TTS), ASM (WebSocket duplex)
- `WithVADMode()` option for voice applications using text-based LLMs
- `WithStreamingConfig()` for native multimodal LLM streaming (Gemini Live)

**Documentation:**
- Updated `docs/src/content/architecture/runtime-pipeline.md` with stage architecture
- Fixed all SDK tutorial pack examples to comply with PromptPack schema v1.1.0

#### SDK v2 - Pack-First Architecture
A complete rewrite of the Go SDK with a pack-first architecture that reduces boilerplate by ~80%.

**New Features:**
- **Pack-First Design**: Load prompts directly from pack files - no manual configuration
- **Simplified API**: 5 lines for hello world vs 30+ in v1
- **Enhanced Variables**: SetVar/GetVar with type safety and concurrent access
- **Streaming Support**: Built-in streaming with customizable handlers
- **Tool System**: Multiple executor types (function, HTTP, MCP)
- **Human-in-the-Loop (HITL)**: Configurable approval workflows for sensitive operations
- **MCP Integration**: Model Context Protocol support via runtime
- **Observability**: EventBus integration with hooks package for monitoring
- **Validation**: Automatic pack validation with detailed error reporting

**New Packages:**
- `sdk/hooks` - Event subscription and observability
- `sdk/tools` - HTTP executor and tool utilities
- `sdk/internal/packloader` - Pack file loading and caching

**Examples:**
- `sdk/examples/hello` - Basic conversation
- `sdk/examples/tools` - Tool registration and execution
- `sdk/examples/streaming` - Token-by-token streaming
- `sdk/examples/hitl` - Human-in-the-loop approval

**Migration:**
See [SDK Migration Guide](docs/sdk-migration.md) for detailed before/after examples.

#### TUI Real-Time Updates
- Real-time conversation updates in TUI with message caching
- Streaming token visualization during LLM responses
- Improved conversation display with turn-by-turn navigation

#### Config Inspect Enhancements
- New `--short` / `-s` flag for quick validation-only output
- New `--section` flag to focus on specific configuration sections:
  - `prompts` - Prompt configurations with task types, variables, validators
  - `providers` - Provider details organized by group (default, judge, selfplay)
  - `scenarios` - Scenario details with turn counts and assertion summaries
  - `tools` - Tool definitions with modes, parameters, timeouts
  - `selfplay` - Self-play configuration including personas and roles
  - `judges` - Judge configurations for LLM-as-judge validators
  - `defaults` - Default settings (temperature, max tokens, concurrency)
  - `validation` - Validation results and connectivity checks
- Rich styled output with lipgloss boxes and Unicode symbols
- Provider grouping display (default, judge, selfplay groups)
- Self-play roles with persona associations
- Connectivity checks showing configuration relationships

#### HTML Reports
- Improved tool rendering in HTML reports with better formatting
- Enhanced tool call display with arguments and results

#### CI/CD
- Added llm-judge, guardrails-test, assertions-test examples to CI pipeline
- Comprehensive test coverage for event handling functions

### Changed
- Refactored event adapter and conversation executor for reduced cognitive complexity
- Extracted anonymous structs to named types for SonarQube compliance
- Extracted duplicated string literals to constants

### Fixed
- Fixed task_type cross-reference validation to check prompt task_types instead of prompt config IDs
- Fixed JUnit reporting to not fail tests when violations are expected
- Allow guardrail violations to pass when assertions expect them
- Fixed assertions-test selfplay configuration

### Documentation
- Added documentation for new event types
- Updated CLI commands reference with config-inspect enhancements

---

### Initial Release Features
- Initial release automation with GoReleaser
- Homebrew tap support
- Multi-platform binary releases

## Release Notes

Detailed release notes will be added for each version.
