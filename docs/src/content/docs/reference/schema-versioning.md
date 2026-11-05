---
title: Schema versioning
description: How the hosted config schemas are versioned, what /latest/ points to, and how schema version relates to apiVersion
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - Makefile
    - pkg/config/loader.go
    - pkg/config/version.go
    - scripts/fetch-schemas.sh
    - scripts/gen-schema-latest-refs.sh
---

The config-file JSON schemas are generated and owned by PromptArena. This site hosts them under two paths, `v1alpha1` and `latest`. For what PromptKit itself checks when it loads config, see [schema validation](/reference/schema-validation/).

## Two versions, two jobs

| Version | Where it appears | Used by |
|---------|------------------|---------|
| Schema version | The `$schema` URL | Editors and external validators |
| API version | The `apiVersion` field | PromptKit's loaders, which require the field to be present |

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
```

The Go constants in `pkg/config/version.go` name both: `APIVersion` is `promptkit.altairalabs.ai/v1alpha1` and `SchemaVersion` is `v1alpha1`.

## Versioned and latest paths

- `https://promptkit.altairalabs.ai/schemas/v1alpha1/<name>.json` is the schema itself.
- `https://promptkit.altairalabs.ai/schemas/latest/<name>.json` is a small file holding a `$ref` to the `v1alpha1` schema of the same name:

```json
{
  "$ref": "https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json"
}
```

The site is static, so `latest` is a `$ref` file rather than an HTTP redirect. The target, fixed in `scripts/gen-schema-latest-refs.sh`, is `v1alpha1`, so `latest` and `v1alpha1` resolve to the same schema. The versioned path never moves to a later version.

## How the site gets the schemas

`make schemas-host`, which the docs build runs, does two things:

1. `scripts/fetch-schemas.sh` downloads the schema files from `schemas/v1alpha1/` in the PromptArena repository into `docs/public/schemas/v1alpha1/`. `PROMPTARENA_SCHEMA_REF` selects the git ref; the default is `main`.
2. `scripts/gen-schema-latest-refs.sh` writes a `$ref` file under `docs/public/schemas/latest/` for each fetched file.

A schema change in PromptArena reaches this site on the next docs build.

PromptArena generates the files with `make schemas` (its `tools/schema-gen`). Its `@altairalabs/promptarena` npm package bundles them under `schemas/` and ships a `schema-map.json` that maps each type to its version, bundled path, hosted `latest` URL and file patterns.

## Related

- [Schema validation](/reference/schema-validation/)
