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
| `content_includes` | Response contains specific text |
| `content_matches` | Response matches regex pattern |
| `tools_called` | Specific tools were invoked |
| `is_valid_json` | Response is valid JSON |
| `json_schema` | Response matches JSON schema |
| `llm_judge` | LLM evaluates response quality |
| `tool_anti_pattern` | Tool calls don't match forbidden sequences |
| `tool_no_repeat` | No unnecessary consecutive duplicate tool calls |
| `tool_efficiency` | Tool call count within budget (max_calls, max_errors, max_error_rate) |
| `cost_budget` | Total cost and tokens within budget |
| `workflow_transition_order` | Workflow states visited in expected order |
| `workflow_tool_access` | Tools only called in permitted workflow states |
| `skill_activated` | Required skills were activated |
| `skill_not_activated` | Forbidden skills were not activated |
| `skill_activation_order` | Skills activated in expected order |
| `invariant_fields_preserved` | Tracked fields not lost between tool calls |
| `outcome_equivalent` | Run outcome matches expected value (tool_calls, final_state, content_hash) |
| `directional` | Perturbation causes expected directional change |

### Dynamic Context Testing

| Feature | Level | Purpose |
|---------|-------|---------|
| `trials` | Scenario | Run scenario N times, aggregate results with pass rates |
| `perturbations` | Turn | Expand turn into variants by substituting placeholders |
| `chaos` | Turn | Inject tool faults (error, timeout, slow) for resilience testing |

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
