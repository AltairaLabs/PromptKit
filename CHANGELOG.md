# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
