---
layout: default
title: PackC
nav_order: 3
has_children: true
permalink: /packc/
---

# 📦 PackC

**PromptPack Compiler - Package and optimize prompts for production**

---

## What is PackC?

PackC is a compiler that helps you:

- **Compile prompts** from YAML/JSON into optimized `.pack.json` files
- **Validate structure** to ensure schema compliance
- **Optimize for production** with minification and preprocessing
- **Version and package** prompts for distribution
- **Integrate with CI/CD** for automated builds

---

## Quick Start

```bash
# Install PackC
make build-packc

# Create a prompt source file
cat > my-prompt.yaml <<EOF
name: "my-app"
version: "1.0.0"
prompts:
  - id: "greeting"
    system: "You are helpful."
    template: "Greet the user."
EOF

# Compile the pack
./bin/packc compile my-prompt.yaml --output my-app.pack.json

# Validate the result
./bin/packc validate my-app.pack.json
```

**Next**: [Your First Compilation Tutorial](/packc/tutorials/01-first-compilation/)

---

## Documentation by Type

### 📚 Tutorials (Learn by Doing)

Step-by-step guides for learning PackC:

1. [First Compilation](/packc/tutorials/01-first-compilation/) - Compile in 5 minutes
2. [CI/CD Pipeline](/packc/tutorials/02-ci-pipeline/) - Automate pack builds

### 🔧 How-To Guides (Accomplish Specific Tasks)

Focused guides for specific PackC tasks:

- [Installation](/packc/how-to/installation/) - Get PackC running
- [Compile Packs](/packc/how-to/compile-packs/) - Compilation options
- [Validate Packs](/packc/how-to/validate-packs/) - Validation strategies
- [Optimize Packs](/packc/how-to/optimize-packs/) - Size and performance
- [Automate Builds](/packc/how-to/automate-builds/) - CI/CD integration

### 💡 Explanation (Understand the Concepts)

Deep dives into PackC design:

- [Why Compile?](/packc/explanation/why-compile/) - Benefits of compilation
- [Pack Format](/packc/explanation/pack-format/) - Understanding .pack.json
- [Optimization](/packc/explanation/optimization/) - How optimization works

### 📖 Reference (Look Up Details)

Complete command and format specifications:

- [CLI Commands](/packc/reference/cli-commands/) - All PackC commands
- [Pack Specification](/packc/reference/pack-specification/) - Schema definition
- [Compiler Options](/packc/reference/compiler-options/) - Configuration flags

---

## Key Features

### Compilation

Transform YAML/JSON prompts into optimized packs:

```bash
packc compile prompts/my-app.yaml \
  --output dist/my-app.pack.json \
  --optimize \
  --version 1.0.0
```

### Validation

Ensure pack integrity:

```bash
packc validate my-app.pack.json --strict
```

Checks:
- ✅ Schema compliance
- ✅ Required fields present
- ✅ Template syntax valid
- ✅ Metadata complete

### Optimization

Production-ready output:

```bash
packc compile prompts/*.yaml --optimize
```

- Minify JSON output
- Remove comments and whitespace
- Validate templates
- Check for common errors

### CI/CD Integration

Automate pack builds:

```yaml
# GitHub Actions
- name: Build Packs
  run: packc compile prompts/*.yaml --output-dir dist/
```

---

## Use Cases

### For DevOps Engineers

- Package prompts for deployment
- Version and distribute packs
- Integrate into build pipelines
- Validate before deployment

### For Release Managers

- Create versioned prompt releases
- Track changes between versions
- Ensure quality with validation
- Distribute to teams

### For Prompt Engineers

- Package tested prompts
- Share prompts with teams
- Ensure consistency
- Prepare for production

---

## CLI Commands

### compile

Compile prompt source files into packs:

```bash
packc compile [files...] [options]
```

Options:
- `--output, -o` - Output file path
- `--output-dir` - Output directory for multiple files
- `--optimize` - Enable optimization
- `--version` - Set pack version
- `--strict` - Fail on warnings

### validate

Validate pack structure:

```bash
packc validate [files...] [options]
```

Options:
- `--strict` - Fail on warnings
- `--schema-version` - Specify schema version
- `--verbose` - Detailed output

### version

Show PackC version:

```bash
packc version
```

---

## Pack Format

A compiled pack is JSON with this structure:

```json
{
  "name": "my-app",
  "version": "1.0.0",
  "description": "My application prompts",
  "prompts": [
    {
      "id": "greeting",
      "system": "You are helpful.",
      "template": "Greet the user.",
      "variables": [],
      "metadata": {}
    }
  ],
  "metadata": {
    "compiled_at": "2025-11-16T12:00:00Z",
    "compiler_version": "1.0.0",
    "schema_version": "2.0"
  }
}
```

---

## Integration Examples

### Makefile

```makefile
.PHONY: build-packs
build-packs:
    packc compile prompts/*.yaml --output-dir dist/packs/

.PHONY: validate-packs
validate-packs:
    packc validate dist/packs/*.pack.json
```

### GitHub Actions

```yaml
- name: Compile PromptPacks
  run: |
    packc compile prompts/*.yaml --output-dir dist/
    packc validate dist/*.pack.json
```

### Docker

```dockerfile
FROM golang:1.22
RUN go install github.com/AltairaLabs/PromptKit/tools/packc@latest
COPY prompts/ /prompts/
RUN packc compile /prompts/*.yaml --output-dir /packs/
```

---

## Best Practices

### Version Management

- Use semantic versioning (MAJOR.MINOR.PATCH)
- Update version on breaking changes
- Keep changelog of prompt changes

### Quality Assurance

- Always run validation after compilation
- Use `--strict` mode in CI/CD
- Test packs before distribution

### Build Optimization

- Cache compiler binary in CI
- Compile only changed files
- Use parallel builds for large projects

---

## Getting Help

- **Quick Start**: [Getting Started Guide](/getting-started/devops-engineer/)
- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)
- **Examples**: [PackC Examples](/packc/examples/)

---

## Related Tools

- **Arena**: [Test prompts before compiling](/arena/)
- **SDK**: [Use compiled packs in applications](/sdk/)
- **Complete Workflow**: [See all tools together](/getting-started/complete-workflow/)
