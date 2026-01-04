---
title: Arena Reference
sidebar:
  order: 0
---
Complete technical specifications and reference materials for PromptArena.

---

## Quick Links

### [CLI Commands](./cli-commands/)
Complete command-line interface reference with all flags and options.

### [Configuration Schema](./config-schema/)
YAML configuration file structure and all available options.

### [Assertions](./assertions/)
All available assertion types for validating LLM responses.

### [Validators](./validators/)
Built-in validators for checking response quality and compliance.

### [Scenario Format](./scenario-format/)
Test scenario file structure and specification.

### [Output Formats](./output-formats/)
Report generation formats (HTML, JSON, JUnit, Markdown).

### [Duplex Configuration](./duplex-config/)
Complete duplex streaming configuration for voice testing scenarios.

---

## Reference vs. How-To

**This is reference documentation** - dry, factual, technical specifications.

Looking for task-oriented guides? See:
- [Arena How-To Guides](/arena/how-to/) - Accomplish specific tasks
- [Arena Tutorials](/arena/tutorials/) - Learn by building

---

## Quick Reference Tables

### Command Summary

| Command | Purpose |
|---------|---------|
| `promptarena run` | Execute test scenarios |
| `promptarena config-inspect` | Validate configuration |
| `promptarena debug` | Debug configuration loading |
| `promptarena prompt-debug` | Test prompt rendering |
| `promptarena render` | Generate reports from results |

### Common Assertions

| Assertion | Purpose |
|-----------|---------|
| `not_empty` | Response is not empty |
| `contains` | Response contains text |
| `matches` | Response matches regex |
| `tool_called` | Specific tool was invoked |
| `max_tokens` | Token count within limit |
| `semantic_similarity` | Meaning matches expected |

### Output Formats

| Format | Use Case |
|--------|----------|
| JSON | Machine processing, APIs |
| HTML | Human-readable reports |
| JUnit | CI/CD integration |
| Markdown | Documentation, sharing |

---

## API Stability

Arena reference documentation follows semantic versioning:

- **Stable**: CLI commands, configuration schema
- **Beta**: Advanced assertions, custom validators
- **Experimental**: New features marked explicitly

---

## Getting Help

- **How-To Guides**: [Task-oriented documentation](/arena/how-to/)
- **Tutorials**: [Learning-oriented guides](/arena/tutorials/)
- **Explanations**: [Conceptual documentation](/arena/explanation/)
- **Issues**: [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues)
