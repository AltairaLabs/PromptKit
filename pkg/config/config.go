// Package config provides configuration management for PromptKit Arena.
//
// This package handles YAML-based configuration loading and validation for:
//   - Test scenarios and conversation definitions
//   - LLM provider settings and credentials
//   - Prompt template configurations
//   - Self-play personas and role assignments
//   - Default parameters and execution settings
//
// Configuration files are loaded from disk with support for file references,
// allowing modular organization of scenarios, providers, and prompts.
//
// The package is organized into:
//   - types.go: Core type definitions (Config, Scenario, Provider, etc.)
//   - loader.go: Loading functions for config files
//   - helpers.go: Utility functions for path resolution
//   - persona.go: Self-play persona loading
//   - validator.go: Configuration validation
package config
