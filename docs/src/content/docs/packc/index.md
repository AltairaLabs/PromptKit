---
title: PackC
description: Compiler for the PromptPack open standard - create portable, framework-agnostic prompt packages
sidebar:
  order: 0
---

**Compiler for the [PromptPack](https://promptpack.org) open standard**

---

## What is PackC?

PackC compiles prompt source files into [PromptPack](https://promptpack.org)-compliant packages—a **vendor-neutral, framework-agnostic** format that works with any AI runtime or provider.

### Why PromptPack?

Today's AI prompt development is fragmented. Each framework has its own format for prompts, tools, conversations, and test scenarios. When teams switch providers or frameworks, they rebuild their entire prompt infrastructure from scratch.

[PromptPack](https://promptpack.org) solves this with an open specification built on three principles:

- **Vendor Neutrality**: A framework-agnostic JSON format that works across any runtime
- **Completeness**: Prompts, tools, guardrails, and resources in a single file
- **Discipline**: Treating prompts as version-controlled, testable engineering artifacts

PackC is the reference compiler for this standard.

### What PackC Does

- **Compiles** YAML/JSON sources into `.pack.json` files conforming to [PromptPack spec](https://promptpack.org)
- **Validates** structure against the official schema
- **Optimizes** for production with minification and preprocessing
- **Versions** packages for distribution and deployment

---

## Quick Start

```bash
# Install PromptKit (includes PackC)
brew install promptkit

# Or with Go
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Create a prompt source file
cat > greeting.yaml <<EOF
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
spec:
  task_type: greeting
  name: Greeting Assistant
  description: A friendly assistant that greets users
  system_prompt: |
    You are a friendly assistant. Greet the user warmly.
  parameters:
    temperature: 0.7
    max_tokens: 150
EOF

# Compile to PromptPack format
packc compile-prompt greeting.yaml --output greeting.pack.json

# Validate against the spec
packc validate greeting.pack.json
```

The resulting `.pack.json` can be used with **any** PromptPack-compatible runtime—not just PromptKit.

**Next**: [Your First Pack Tutorial](/packc/tutorials/01-first-pack/)

---

## Framework Agnostic

A key benefit of PromptPack is **portability**. Packs compiled with PackC work with:

- **PromptKit SDK** (Go)
- **Any PromptPack-compatible runtime** in other languages
- **Custom integrations** that read the standard JSON format

Build once, deploy everywhere. No vendor lock-in.

```d2
direction: right

sources: YAML Sources {
  shape: rectangle
  label: "YAML Sources\n(your prompts)"
}

packc: PackC {
  shape: rectangle
  label: "PackC\n(compiler)"
}

pack: .pack.json {
  shape: rectangle
  label: ".pack.json\n(PromptPack)"
}

promptkit: PromptKit (Go SDK)
other: Other Frameworks
custom: Custom Integration

sources -> packc -> pack
pack -> promptkit
pack -> other
pack -> custom
```

---

## Documentation by Type

### 📚 Tutorials (Learn by Doing)

Step-by-step guides for learning PackC:

1. [First Pack](/packc/tutorials/01-first-pack/) - Create your first PromptPack
2. [Multi-Prompt Packs](/packc/tutorials/02-multi-prompt/) - Bundle multiple prompts
3. [Validation Workflow](/packc/tutorials/03-validation-workflow/) - Ensure pack quality
4. [Pack Management](/packc/tutorials/04-pack-management/) - Organize and version packs
5. [CI/CD Pipeline](/packc/tutorials/05-ci-cd-pipeline/) - Automate pack builds

### 🔧 How-To Guides (Accomplish Specific Tasks)

Focused guides for specific tasks:

- [Installation](/packc/how-to/install/) - Get PackC running
- [Compile Packs](/packc/how-to/compile-packs/) - Compilation options
- [Validate Packs](/packc/how-to/validate-packs/) - Validation strategies
- [Organize Packs](/packc/how-to/organize-packs/) - Project structure
- [CI/CD Integration](/packc/how-to/ci-cd-integration/) - Automate builds

### 💡 Explanation (Understand the Concepts)

Deep dives into PackC and PromptPack:

- [Pack Format](/packc/explanation/pack-format/) - Understanding the PromptPack structure
- [Compilation](/packc/explanation/compilation/) - How compilation works
- [Validation](/packc/explanation/validation/) - Schema validation details

### 📖 Reference (Look Up Details)

Complete command and format specifications:

- [compile](/packc/reference/compile/) - Compile command reference
- [validate](/packc/reference/validate/) - Validate command reference
- [inspect](/packc/reference/inspect/) - Inspect command reference
- [compile-prompt](/packc/reference/compile-prompt/) - Single prompt compilation

---

## Key Features

### Compilation to Open Standard

Transform YAML/JSON prompts into [PromptPack](https://promptpack.org)-compliant packages:

```bash
packc compile prompts/my-app.yaml \
  --output dist/my-app.pack.json \
  --optimize \
  --version 1.0.0
```

### Schema Validation

Ensure packs conform to the [PromptPack specification](https://promptpack.org):

```bash
packc validate my-app.pack.json --strict
```

Checks:
- ✅ Schema compliance with PromptPack spec
- ✅ Required fields present
- ✅ Template syntax valid
- ✅ Tool definitions complete

### Production Optimization

Production-ready output:

```bash
packc compile prompts/*.yaml --optimize
```

- Minify JSON output
- Remove comments and whitespace
- Validate templates
- Check for common errors

---

## PromptPack Format

A compiled pack follows the [PromptPack specification](https://promptpack.org):

```json
{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptPack",
  "metadata": {
    "name": "my-app",
    "version": "1.0.0"
  },
  "prompts": [
    {
      "id": "greeting",
      "system": "You are helpful.",
      "template": "Greet the user."
    }
  ]
}
```

This format is:
- **Self-contained**: Everything needed to run the prompt
- **Portable**: Works with any compatible runtime
- **Versionable**: Track changes with semantic versioning
- **Testable**: Include test metadata for validation

Learn more at [promptpack.org](https://promptpack.org).

---

## CI/CD Integration

Automate pack builds in your pipeline:

### GitHub Actions

```yaml
- name: Compile PromptPacks
  run: |
    packc compile prompts/*.yaml --output-dir dist/
    packc validate dist/*.pack.json --strict
```

### Makefile

```makefile
.PHONY: build-packs
build-packs:
    packc compile prompts/*.yaml --output-dir dist/packs/

.PHONY: validate-packs
validate-packs:
    packc validate dist/packs/*.pack.json --strict
```

---

## Best Practices

### Use the Open Standard

- Follow the [PromptPack specification](https://promptpack.org) for maximum portability
- Don't add custom fields outside the spec
- Test packs with multiple runtimes if possible

### Version Management

- Use semantic versioning (MAJOR.MINOR.PATCH)
- Update version on breaking changes
- Keep changelog of prompt changes

### Quality Assurance

- Always validate after compilation
- Use `--strict` mode in CI/CD
- Test packs with Arena before distribution

---

## Resources

- **PromptPack Specification**: [promptpack.org](https://promptpack.org)
- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)

---

## Related Tools

- **Arena**: [Test prompts before compiling](/arena/) - Validate prompt behavior
- **SDK**: [Use compiled packs in Go applications](/sdk/) - One of many possible runtimes
