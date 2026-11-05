---
title: Configuration schema validation
description: What PromptKit checks when it loads config files, and the JSON schemas this site hosts for editors and CI
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - Makefile
    - pkg/config/loader.go
    - pkg/config/schema_validator.go
    - runtime/prompt/schema/schema.go
    - scripts/fetch-schemas.sh
    - scripts/gen-schema-latest-refs.sh
    - sdk/internal/pack/validate.go
    - sdk/provider_file.go
---

PromptKit checks configuration files against its Go types. The JSON schemas under `/schemas/` are published by PromptArena, which generates them; this site only hosts them for editor support and external validation.

## What PromptKit validates

PromptKit does not ship config-file JSON schemas and does not validate against them. Its loaders decode strictly and then run Go checks.

For a provider file loaded by `sdk.WithProviderFile` or `sdk.WithProvidersDir`, the load fails when:

- the file is not a YAML mapping
- `apiVersion`, `kind` or `spec` is missing
- a key is not a field of the provider type (a misspelled or unsupported setting)
- `spec.type` is missing
- `spec.role` is not a known role

A top-level `$schema` key is allowed and ignored. Because the check runs against the Go types, a provider setting works from the PromptKit release that adds it, before any published schema includes it.

The `Validate*` functions and `UseSchemaFS` in `pkg/config` are deprecated. They validate only against schemas the caller supplies.

The embedded PromptPack schema in `runtime/prompt/schema` is a separate matter: it is a verbatim copy of the PromptPack specification, and pack loading validates against it.

## Hosted schemas

The docs build fetches the schemas from the PromptArena repository (`make schemas-host`), so they are only as current as the last docs deploy. They are served at `https://promptkit.altairalabs.ai/schemas/`.

| Path | Meaning |
|------|---------|
| `/schemas/v1alpha1/<name>.json` | The versioned schema |
| `/schemas/latest/<name>.json` | A file containing a `$ref` to the `v1alpha1` schema of the same name |

The files are `arena`, `scenario`, `provider`, `promptconfig`, `tool`, `persona`, `eval`, `logging` and `runtime-config`, plus `common/metadata`, `common/assertions` and `common/media`, each with a `.json` extension.

For the field-by-field contents, read the schema file itself or the Go types in `pkg/config`.

## `$schema` key

A `$schema` key as the first key selects the schema:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/provider.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4
spec:
  id: openai-gpt4
  type: openai
  model: gpt-4
```

`$schema` selects the schema version your editor uses. `apiVersion` is a separate field that the loaders require; see [schema versioning](/reference/schema-versioning/).

## Editor schema mapping

Editors that use the Red Hat YAML language server (VS Code, Neovim, Emacs) take a glob-to-schema map:

```json
{
  "yaml.schemas": {
    "https://promptkit.altairalabs.ai/schemas/latest/provider.json": ["*.provider.yaml"],
    "https://promptkit.altairalabs.ai/schemas/latest/scenario.json": ["*.scenario.yaml"],
    "https://promptkit.altairalabs.ai/schemas/latest/arena.json": ["*.arena.yaml"]
  }
}
```

`sdk.WithProvidersDir` loads every `*.provider.yaml` in a directory, so that suffix is the one the SDK itself relies on. The file patterns PromptArena associates with each schema (`*.arena.yaml`, `*.scenario.yaml`, `*.provider.yaml`, `*.prompt.yaml`, `*.tool.yaml`, `*.persona.yaml`, and the matching `scenarios/`, `providers/`, `prompts/`, `tools/` and `personas/` directories) are listed in the `schema-map.json` that ships in its `@altairalabs/promptarena` npm package.

## `promptarena validate`

The `promptarena validate [file]` command checks a file against the schema for its type and, for an arena config, runs further checks that the config builds a valid pack. Its flags:

| Flag | Meaning |
|------|---------|
| `--type` | `auto` (default), `arena`, `scenario`, `provider`, `promptconfig`, `tool` or `persona` |
| `--schema-only` | Skip the checks beyond the schema |
| `--verbose` | Show every validation error |
| `--json` | Emit the result as JSON |

The command has no flag for choosing a schema location. The `@altairalabs/promptarena` npm package also bundles the schemas for offline use. PromptArena owns the command, the package and the schema generator (`make schemas` in its repository); see its documentation for details.

Any JSON Schema validator can also check a file against a hosted schema.

## Related

- [Schema versioning](/reference/schema-versioning/)
- [Provider configuration](/runtime/reference/providers/)
