---
id: schema-validation
title: Validate against the binary's own schemas
summary: Run `promptarena schema <type>` for authoritative structure and `promptarena validate` to check configs.
tags: [schema, validation]
---
Every config type (scenario, provider, prompt, tool, arena) has a JSON schema. The
schema embedded in your installed `promptarena` binary is the source of truth — it is
the exact version `promptarena validate` enforces. Prefer it over the public web copy,
which may be a different release.

- `promptarena schema <type>` — print the authoritative schema for a type.
- `promptarena validate` — check your configs before running.

Author configs to the schema first; don't guess field names.
