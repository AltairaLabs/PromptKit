---
title: Schema Versioning & Discoverability
description: How PromptKit manages schema versions and enables automatic schema discovery
sidebar:
  order: 6
---

PromptKit uses a robust schema versioning system to ensure stability while enabling automatic updates.

## Version Aliasing

### Using `/latest/` for Automatic Updates

The recommended approach is to use `/latest/` in your `$schema` URLs:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
```

**How it works:**

- The `/latest/` path uses JSON Schema's `$ref` mechanism
- Automatically points to the current stable schema version
- No HTTP redirects needed - works on static hosting
- Updates automatically when new schema versions are released

**Benefits:**

- Always get the latest validation rules
- Automatic field documentation updates in your IDE
- No manual URL updates needed
- Backwards compatible with your runtime `apiVersion`

### Version Separation

PromptKit separates schema versions from API versions:

```yaml
# Schema URL - can use /latest/ for automatic updates
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json

# API version - remains stable for runtime compatibility
apiVersion: promptkit.altairalabs.ai/v1alpha1
```

**Why separate them?**

- **Schema URL**: For IDE and validation - can update automatically
- **API Version**: For runtime - must remain stable for existing deployments
- Allows schema improvements without breaking running systems

## Versioned Schemas

When you need stability (e.g., CI/CD, production), use explicit versions:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
```

**Use versioned schemas when:**

- Running in CI/CD pipelines
- Deploying to production
- Need guaranteed schema stability
- Testing against specific schema versions

## Version Management

### Centralized Version Constants

Versions are managed in Go code:

```go
// pkg/config/version.go
const (
    APIVersion    = "promptkit.altairalabs.ai/v1alpha1"
    SchemaVersion = "v1alpha1"
)
```

### Schema Generation

Schemas are automatically generated during the build process:

```bash
# Generate schemas for current version
make schemas

# Outputs to:
#   schemas/v1alpha1/       # Versioned schemas
#   docs/public/schemas/latest/  # Latest aliases
```

### Latest Schema Structure

The `/latest/` schemas use `$ref` to point to versioned schemas:

```json
{
  "$ref": "https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json"
}
```

This approach:

- Works on static hosting (GitHub Pages)
- No server-side redirects needed
- Follows JSON Schema best practices
- Supported by all schema-aware tools

## NPM Package Integration

The `@altairalabs/promptarena` npm package includes bundled schemas for offline use.

### Schema Metadata

The package includes `schema-map.json` with metadata:

```json
{
  "schemaVersion": "1.0.0",
  "schemas": {
    "arena": {
      "version": "v1alpha1",
      "path": "./schemas/arena.json",
      "url": "https://promptkit.altairalabs.ai/schemas/latest/arena.json",
      "filePatterns": ["config.arena.yaml", "*.arena.yaml"]
    }
  }
}
```

### Programmatic Access

Import schemas in JavaScript/TypeScript:

```typescript
import schemaMap from '@altairalabs/promptarena/schema-map';
import arenaSchema from '@altairalabs/promptarena/schemas/arena.json';

// Get schema URL for a type
const schemaUrl = schemaMap.schemas.arena.url;

// Use bundled schema offline
const validator = new Ajv().compile(arenaSchema);
```

## File Naming for Schema Discovery

PromptKit uses typed file extensions to enable automatic schema matching:

| Type | File Pattern | Schema URL |
|------|-------------|-----------|
| Arena | `config.arena.yaml` | `/schemas/latest/arena.json` |
| Scenario | `*.scenario.yaml` | `/schemas/latest/scenario.json` |
| Provider | `*.provider.yaml` | `/schemas/latest/provider.json` |
| Tool | `*.tool.yaml` | `/schemas/latest/tool.json` |
| Persona | `*.persona.yaml` | `/schemas/latest/persona.json` |

This enables:

- **Schema Store Integration**: Submit patterns to [schemastore.org](https://schemastore.org)
- **IDE Auto-detection**: Language servers can match files by pattern
- **Batch Validation**: Find all files of a type easily

### Schema Store Submission

PromptKit schemas can be submitted to the [JSON Schema Store](https://www.schemastore.org/) for automatic IDE integration:

```json
{
  "name": "PromptKit Arena Configuration",
  "description": "Configuration file for PromptKit Arena testing framework",
  "fileMatch": ["config.arena.yaml", "*.arena.yaml"],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/arena.json"
}
```

Once accepted, IDEs will automatically suggest PromptKit schemas for matching files.

## Migration Guide

### Updating Existing Configs

1. **Add `/latest/` to schema URLs:**

   ```diff
   - $schema: https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json
   + $schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
   ```

2. **Rename files with typed extensions:**

   ```bash
   mv arena.yaml config.arena.yaml
   mv providers/openai.yaml providers/openai.provider.yaml
   mv scenarios/test.yaml scenarios/test.scenario.yaml
   ```

3. **Update file references in configs:**

   ```diff
   scenarios:
   -   - file: scenarios/test.yaml
   +   - file: scenarios/test.scenario.yaml
   ```

### Bulk Update Script

```bash
#!/bin/bash
# Update all configs to use /latest/ schemas

find . -name "*.yaml" -type f -exec sed -i '' \
  's|schemas/v1alpha1/|schemas/latest/|g' {} \;

# Rename files with typed extensions
find . -name "arena.yaml" -exec \
  bash -c 'mv "$1" "$(dirname "$1")/config.arena.yaml"' _ {} \;
  
find . -path "*/providers/*.yaml" ! -name "*.provider.yaml" -exec \
  bash -c 'mv "$1" "${1%.yaml}.provider.yaml"' _ {} \;
```

## Version Release Process

When releasing a new schema version:

1. **Update version constants:**

   ```go
   const SchemaVersion = "v1alpha2"
   ```

2. **Generate new schemas:**

   ```bash
   make schemas
   ```

3. **Update `/latest/` references:**

   ```json
   {
     "$ref": "https://promptkit.altairalabs.ai/schemas/v1alpha2/arena.json"
   }
   ```

4. **Publish to npm:**

   ```bash
   cd npm/promptarena
   npm version patch
   npm publish
   ```

## Best Practices

1. **Use `/latest/` in development**: Get automatic updates and improvements
2. **Pin versions in CI/CD**: Ensure reproducible builds
3. **Test before updating**: Run `promptarena validate` after schema updates
4. **Document breaking changes**: Note any fields removed or changed
5. **Use typed file extensions**: Enable better IDE integration

## Troubleshooting

### Schema Not Found

If you get 404 errors for schemas:

```text
Error: Failed to load schema from https://promptkit.altairalabs.ai/schemas/latest/arena.json
```

**Solutions:**

1. Check you have internet access
2. Verify the schema URL is correct
3. Use local schemas for offline work: `promptarena validate --local-schemas`

### Schema Validation Fails After Update

If validation fails after updating schemas:

1. **Check the changelog** for breaking changes
2. **Run with `--verbose`** to see detailed errors:

   ```bash
   promptarena validate config.arena.yaml --verbose
   ```

3. **Compare with examples:**

   ```bash
   ls examples/assertions-test/*.arena.yaml
   ```

### IDE Not Detecting Schemas

1. **Check file naming** matches patterns
2. **Verify VS Code settings** include typed extensions
3. **Reload IDE** after changing settings
4. **Clear schema cache**:

   ```bash
   rm -rf ~/.vscode/schemas
   ```

## Related Documentation

- [Schema Validation](/reference/schema-validation/) - Using schemas for validation
- [Configuration Reference](/arena/reference/config-schema/) - Arena config structure
- [CI/CD Integration](/arena/how-to/ci-cd-integration/) - Automating validation
