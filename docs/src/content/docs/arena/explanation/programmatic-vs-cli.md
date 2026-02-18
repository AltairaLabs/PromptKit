---
title: Programmatic vs CLI Usage
description: Understanding when to use Arena as a library versus the CLI
---

Arena can be used in two ways: as a command-line tool or as a Go library. This guide explains the differences, trade-offs, and when to choose each approach.

## Overview

### CLI Approach

```bash
promptarena run config.arena.yaml
```

**What it is:** Using Arena as a standalone command-line tool with YAML configuration files.

### Programmatic Approach

```go
eng, _ := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
runIDs, _ := eng.ExecuteRuns(ctx, plan, 4)
```

**What it is:** Importing Arena as a Go library and controlling it programmatically.

## Decision Matrix

| Use Case | CLI | Programmatic | Why |
|----------|-----|--------------|-----|
| Quick manual testing | ✅ Best | ⚠️ Overkill | CLI is faster for ad-hoc testing |
| CI/CD pipelines | ✅ Good | ✅ Good | Both work well, CLI is simpler |
| Integration with apps | ❌ Limited | ✅ Best | Need programmatic control |
| Dynamic test generation | ❌ Hard | ✅ Best | Can't easily generate YAML |
| Custom result processing | ⚠️ Via scripts | ✅ Best | Direct access to results |
| Team collaboration | ✅ Best | ⚠️ Harder | YAML files are git-friendly |
| Complex workflows | ⚠️ Limited | ✅ Best | Need conditional logic |
| Learning Arena | ✅ Best | ⚠️ Harder | CLI has gentler learning curve |

## CLI: When to Use

### ✅ Good For:

#### 1. Manual Testing and Exploration

```bash
# Quick iteration on prompts
promptarena run --scenario greeting-test
promptarena run --provider claude
promptarena run --verbose
```

**Why:** Immediate feedback, no compilation needed.

#### 2. Standard CI/CD Integration

```yaml
# .github/workflows/test.yml
- name: Run Arena tests
  run: promptarena run --ci --format junit
```

**Why:** Simple, declarative, version-controlled configuration.

#### 3. Team Collaboration

```yaml
# config.arena.yaml (committed to git)
scenarios:
  - file: scenarios/customer-support.yaml
  - file: scenarios/edge-cases.yaml
```

**Why:** Everyone uses the same config, changes are tracked, reviews are easy.

#### 4. Interactive TUI

```bash
# Watch tests execute in real-time
promptarena run
```

**Why:** Beautiful terminal UI with progress, logs, and results.

### ❌ Not Good For:

#### Dynamic Test Generation

```bash
# Can't do this with CLI:
# Generate 1000 test scenarios from database
# Run tests based on runtime conditions
```

#### Custom Result Processing

```bash
# Limited to built-in outputs:
# - JSON, HTML, JUnit, Markdown
# Can't easily pipe to custom analytics
```

#### Application Integration

```bash
# Can't embed in your app:
# Have to shell out and parse output
system("promptarena run ...")
```

## Programmatic: When to Use

### ✅ Good For:

#### 1. Integration with Applications

```go
// Embed testing in your app
func validateUserPrompt(prompt string) error {
    cfg := buildTestConfig(prompt)
    eng, _ := setupEngine(cfg)
    results := runTests(eng)
    return validateResults(results)
}
```

**Why:** Direct integration, no external processes.

#### 2. Dynamic Test Generation

```go
// Generate scenarios from data
scenarios := make(map[string]*config.Scenario)
for _, conversation := range loadFromDB() {
    scenarios[conversation.ID] = buildScenario(conversation)
}
cfg.LoadedScenarios = scenarios
```

**Why:** Can generate tests programmatically.

#### 3. Custom Workflows

```go
// Complex conditional logic
for attempt := 0; attempt < maxRetries; attempt++ {
    results := runTests(eng)
    if allPassed(results) {
        break
    }
    adjustConfig(cfg, results)
}
```

**Why:** Full control over execution flow.

#### 4. Custom Result Processing

```go
// Send metrics to your system
for _, result := range results {
    metrics := extractMetrics(result)
    sendToDatadog(metrics)
    updateDashboard(metrics)
}
```

**Why:** Direct access to result objects.

#### 5. Building Testing Tools

```go
// Custom testing frameworks
type TestRunner struct {
    engine *engine.Engine
}

func (r *TestRunner) RunWithRetry(...)
func (r *TestRunner) CompareProviders(...)
func (r *TestRunner) GenerateReport(...)
```

**Why:** Build specialized tools on top of Arena.

### ❌ Not Good For:

#### Quick Iteration

```go
// Every change requires:
// 1. Edit code
// 2. Recompile
// 3. Run
// vs CLI: just edit YAML and run
```

#### Team Onboarding

```go
// Steeper learning curve:
// - Need to know Go
// - Understand API
// - Set up dev environment
// vs CLI: just install and run
```

## Hybrid Approach

You can combine both approaches:

### Pattern: CLI for Standard Tests, Code for Custom

```bash
# Standard regression tests (CLI)
promptarena run regression-tests.yaml --ci

# Custom dynamic tests (programmatic)
go run custom-tests/main.go
```

### Pattern: Generate Config, Run with CLI

```go
// Generate config file
cfg := buildConfigFromData()
saveToYAML(cfg, "generated.arena.yaml")

// Run with CLI
exec.Command("promptarena", "run", "generated.arena.yaml").Run()
```

### Pattern: CLI for Development, Programmatic for Production

```bash
# During development
promptarena run --verbose

# In production CI/CD
# Use Go binary with custom logic
./custom-test-runner
```

## Migration Path

### From CLI to Programmatic

If you outgrow the CLI:

1. **Keep existing YAML configs**
   ```go
   // Load existing configs
   eng, _ := engine.NewEngineFromConfigFile("config.arena.yaml")
   ```

2. **Gradually add programmatic logic**
   ```go
   // Start with file loading
   eng, _ := engine.NewEngineFromConfigFile("config.arena.yaml")
   
   // Add custom processing
   results := executeAndProcess(eng)
   ```

3. **Eventually go fully programmatic**
   ```go
   // Build config in code
   cfg := buildConfig()
   eng, _ := setupEngine(cfg)
   ```

## Performance Considerations

### CLI

- **Startup time:** ~100-500ms (Go binary startup)
- **Overhead:** Minimal (one process)
- **Memory:** Isolated per execution

### Programmatic

- **Startup time:** 0ms (in same process)
- **Overhead:** None (native function calls)
- **Memory:** Shared with application

**Winner:** Programmatic for high-frequency testing.

## Debugging

### CLI

```bash
# Easy debugging
promptarena run --verbose
promptarena debug --config arena.yaml
promptarena config-inspect
```

### Programmatic

```go
// Standard Go debugging
log.Printf("Config: %+v", cfg)
err := eng.ExecuteRuns(ctx, plan, 4)
log.Printf("Results: %+v", results)
```

**Winner:** CLI for quick troubleshooting, programmatic for deep debugging.

## Recommendations

### Start with CLI if:

- ✅ You're new to Arena
- ✅ You have standard testing needs
- ✅ Your tests are stable and predefined
- ✅ You want quick results
- ✅ Your team isn't familiar with Go

### Use Programmatic if:

- ✅ You need to integrate with applications
- ✅ You need dynamic test generation
- ✅ You need custom result processing
- ✅ You're building testing tools
- ✅ You have complex conditional workflows
- ✅ You need high-frequency testing

### Use Both if:

- ✅ You want flexibility
- ✅ You have different use cases
- ✅ Some tests are standard, some are custom
- ✅ You want CLI for development, code for production

## Real-World Examples

### E-commerce: CLI Approach

```bash
# Test product recommendations daily
promptarena run product-tests.yaml --ci
```

**Why:** Standard tests, CI/CD integration, team collaboration.

### SaaS Platform: Programmatic Approach

```go
// Test each customer's custom prompt
for _, customer := range customers {
    cfg := buildConfigForCustomer(customer)
    results := runTests(cfg)
    notifyCustomer(customer, results)
}
```

**Why:** Dynamic per-customer testing, custom notifications.

### AI Research: Hybrid Approach

```bash
# Standard benchmarks (CLI)
promptarena run benchmarks/*.yaml

# Experimental tests (programmatic)
go run experiments/ablation-study.go
```

**Why:** Standard tests for baselines, code for experiments.

## Summary

| Aspect | CLI | Programmatic |
|--------|-----|--------------|
| **Ease of use** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Flexibility** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Team collaboration** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Integration** | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Custom logic** | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Learning curve** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Performance** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

**Golden Rule:** Start with CLI. Switch to programmatic when you hit limitations.

## See Also

- [Tutorial: Programmatic Usage](../tutorials/07-programmatic-usage)
- [How-To: Use as Go Library](../how-to/use-as-go-library)
- [Reference: CLI Commands](../reference/cli-commands)
- [Reference: API Reference](../reference/api-reference)
