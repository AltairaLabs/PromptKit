---
title: Validation Strategy
sidebar:
  order: 3
---
Understanding PackC's approach to pack validation and quality assurance.

## Overview

PackC implements multi-level validation to catch errors early and ensure pack quality before deployment.

## Validation Philosophy

### Fail Fast

Catch errors at compile time, not runtime:

```yaml
# Invalid prompt caught by packc
task_type: ""  # Error: task_type cannot be empty
```

Better than runtime failure:

```go
// Runtime error (bad!)
conv, err := manager.NewConversation(ctx, pack, config)
// Error: prompt not found
```

### Progressive Validation

Multiple checkpoints throughout compilation:

```
Parse → Validate Syntax → Validate Structure → Validate References → Final Check
```

Each stage catches different error types.

### Warnings vs. Errors

**Errors** - Fatal, stop compilation:

```
Error: Missing required field 'task_type'
Error: Invalid template syntax
```

**Warnings** - Non-fatal, allow compilation:

```
Warning: Missing description for prompt 'support'
Warning: Tool 'search' not defined
```

Warnings indicate potential issues that should be reviewed.

## Validation Levels

### Level 1: Syntax Validation

**What**: Valid YAML/JSON syntax

**When**: During parsing

**Examples**:

```yaml
# Invalid YAML
prompts
  - support.yaml  # Missing colon
```

**Error**:

```
Error parsing config: yaml: line 2: did not find expected key
```

### Level 2: Schema Validation

**What**: Required fields present, correct types

**When**: After parsing

**Check**:

```go
type Spec struct {
  TaskType       string `yaml:"task_type"`       // Required
  SystemTemplate string `yaml:"system_template"` // Required
  Version        string `yaml:"version"`         // Required
}
```

**Errors**:

```
Error: Missing required field 'task_type'
Error: Field 'temperature' must be number, got string
```

### Level 3: Template Validation

**What**: Template syntax valid

**When**: During compilation

**Check**:

```go
tmpl, err := template.New("test").Parse(userTemplate)
if err != nil {
  return fmt.Errorf("Invalid template: %w", err)
}
```

**Examples**:

```yaml
# Invalid Go template
system_template: "
```

**Error**:

```
Error: Template parse error: unclosed action
```

### Level 4: Reference Validation

**What**: Referenced resources exist

**When**: After pack assembly

**Check**:
- Tools referenced in prompts are defined
- Fragments referenced exist
- Media files exist on disk

**Warnings**:

```
Warning: Tool 'search_kb' referenced but not defined
Warning: Image file not found: images/logo.png
```

### Level 5: Workflow Validation

**What**: Workflow state machine integrity

**When**: After reference validation

**Check**:
- Entry state exists in the states map
- All transition targets reference valid states
- Each state's `prompt_task` references a prompt in the pack
- Orchestration and persistence values are valid enums
- No unreachable states

**Errors**:

```
Error: workflow entry state "unknown" not found in states
Error: state "intake" transition "Escalate" targets unknown state "missing"
```

**Warnings**:

```
Warning: state "archived" is unreachable from entry state
Warning: state "specialist" references unknown prompt_task "missing-prompt"
```

### Level 6: Semantic Validation

**What**: Logical consistency

**When**: Final validation pass

**Check**:
- No duplicate prompt IDs
- No circular fragment references
- Reasonable parameter values
- Pack size within limits

**Warnings**:

```
Warning: temperature=2.0 exceeds recommended max of 1.0
Warning: Pack size 2MB exceeds recommended 1MB
```

## Validation Rules

### Required Fields

**Pack Level**:

```json
{
  "id": "required",
  "version": "required",
  "prompts": "required"
}
```

**Prompt Level**:

```json
{
  "id": "required",
  "name": "optional but recommended",
  "description": "optional but recommended",
  "version": "required",
  "system_template": "required",
  "variables": "optional but recommended"
}
```

### Type Validation

**String fields**:

```yaml
task_type: "support"  # ✅ Valid
task_type: 123        # ❌ Error: must be string
```

**Numeric parameters**:

```yaml
temperature: 0.7      # ✅ Valid
temperature: "0.7"    # ❌ Error: must be number
```

**Boolean fields**:

```yaml
debug: true          # ✅ Valid
debug: "true"        # ❌ Error: must be boolean
```

### Range Validation

**Temperature**: 0.0 to 2.0

```yaml
temperature: 0.7     # ✅ Valid
temperature: -1.0    # ⚠️ Warning: below 0
temperature: 3.0     # ⚠️ Warning: above 2.0
```

**Max tokens**: 1 to 100,000

```yaml
max_tokens: 1000     # ✅ Valid
max_tokens: 0        # ❌ Error: must be positive
max_tokens: 200000   # ⚠️ Warning: very large
```

### Pattern Validation

**Pack ID**: Alphanumeric with hyphens

```yaml
id: "customer-support"     # ✅ Valid
id: "Customer Support!"    # ❌ Error: invalid characters
```

**Task Type**: Alphanumeric with hyphens/underscores

```yaml
task_type: "support-agent"  # ✅ Valid
task_type: "support agent"  # ❌ Error: no spaces
```

### Template Validation

**Go Templates**:

```yaml
# Valid
system_template: "User: {{user_name}}"
system_template: "{{message}}"

# Invalid
system_template: "{{.unclosed"
system_template: "{{unknown_func .x}}"  # Unknown function
```

**Variable References**:

```yaml
# Warning if variables not documented
system_template: "{{undocumented_var}}"  # Variable not in variables list
```

## Validation Implementation

### Validation Pipeline

```go
func (p *Pack) Validate() []string {
  warnings := []string{}
  warnings = append(warnings, p.validatePackFields()...)
  warnings = append(warnings, p.validateTemplateEngine()...)
  warnings = append(warnings, p.validatePrompts()...)
  warnings = append(warnings, p.validateCompilation()...)
  return warnings
}
```

### Validation Output

Validation returns a list of warning strings:

```go
warnings := pack.Validate()
// Returns: []string{"missing required field: id", "prompt 'support': missing system_template"}
```

**Example output**:

```
⚠ Pack has 2 warnings:
  - missing required field: id
  - prompt 'support': missing system_template
```

## Validation Strategies

### Default Mode

Allow warnings:

```bash
packc compile
```

Warnings are reported but do not stop compilation. Use for development and production builds alike. Review warnings to improve pack quality.

## Pre-Deployment Validation

### Validation Checklist

Before deploying to production:

**1. Schema valid**

```bash
packc validate pack.json
```

**2. Size acceptable**

```bash
du -h pack.json
# Should be < 1MB
```

**3. JSON valid**

```bash
jq empty pack.json
```

**4. Required metadata present**

```bash
jq -e '.compilation.compiled_with' pack.json
jq -e '.version' pack.json
```

**5. Load test**

```bash
# Test with SDK
go run test-pack.go pack.json
```

### Automated Validation

In CI/CD:

```yaml
- name: Validate packs
  run: |
    packc compile --config arena.yaml --output pack.json --id app
    packc validate pack.json
    
    # Additional checks
    ./scripts/check-pack-size.sh pack.json
    ./scripts/test-pack-load.sh pack.json
```

## Validation Reports

### Summary Report

```bash
packc validate pack.json
```

**Output**:

```
Validating pack: pack.json

Pack Information:
  ID: customer-support
  Version: 1.0.0
  Prompts: 3

✓ Schema valid
✓ All prompts valid
✓ All references valid

⚠️ Warnings: 2
  - Missing description for prompt 'support'
  - Tool 'search_kb' referenced but not defined

Summary: Pack is valid with 2 warnings
```

### Detailed Report (future)

A more detailed validation report may be added in future versions.

**Example of what a detailed report might look like**:

```
Validation Report
=================

Pack: customer-support v1.0.0
Compiler: packc-v0.1.0
Validated: 2025-01-16T10:30:00Z

Schema Validation: ✅ PASS
  - Pack ID present
  - Version valid (1.0.0)
  - 3 prompts found

Prompt Validation: ✅ PASS
  - greeting: ✅ Valid
  - support: ⚠️ Missing description
  - escalation: ✅ Valid

Reference Validation: ⚠️ WARNINGS
  - Tool 'search_kb': not defined
  - Tool 'create_ticket': not defined

Template Validation: ✅ PASS
  - All templates syntactically valid
  - 2 variables used: name, message

Fragment Validation: ✅ PASS
  - No fragments used

Size Analysis:
  - Pack size: 45 KB
  - Estimated load time: 5ms
  - Memory footprint: ~100 KB

Overall: ✅ VALID (2 warnings)
```

## Validation Best Practices

### 1. Validate Early

```bash
# During development
packc compile && packc validate
```

### 2. Validate Often

```bash
# Pre-commit hook
packc validate pack.json || exit 1
```

### 3. Automate Validation

```makefile
.PHONY: validate
validate:
	packc validate packs/*.pack.json
```

### 4. Track Warnings

```bash
# Log warnings
packc validate pack.json 2>&1 | tee validation.log
```

### 5. Fix Warnings

Don't ignore warnings - they indicate potential issues:

```yaml
# Bad
task_type: support
# Warning: Missing description

# Good
task_type: support
description: "Handles customer support inquiries"
```

## Validation Evolution

### Current (v0.1.0)

- Schema validation
- Template syntax checking
- Basic reference checking
- Size warnings

### Planned (v0.2.0)

- Semantic validation
- Custom validation rules
- Validation profiles
- Detailed reports

### Future (v1.0.0)

- ML-based validation (detect poor prompts)
- Security scanning
- Performance predictions
- Quality scoring

## Summary

PackC's validation strategy:

- **Multi-level** - Syntax, schema, references, semantics
- **Progressive** - Validation at each pipeline stage
- **Informative** - Clear error messages with context
- **Flexible** - Warnings vs. errors
- **Automated** - Easy to integrate in workflows
- **Evolving** - Continuous improvement

This ensures packs are correct, complete, and production-ready before deployment.
