---
title: Developing PromptArena Templates
description: How to build, test, and publish templates for the community repo.
---

## Prerequisites
- Node.js + npm (for the PromptArena CLI)
- Go toolchain (to run template-related tests locally)
- A clone of the template repo (e.g., `../promptkit-templates`)

## Workflow
1. **Create a template package**
   - Add a new directory under the template repo (matching the template name).
   - Add `template.yaml` with `apiVersion: promptkit.altairalabs.ai/v1alpha1`, `kind: Template`, `spec.files`, and any `variables`.
   - Prefer `files[].source` to keep larger content in separate files; set `BaseDir` by keeping files beside `template.yaml`.

2. **Update the index**
   - Edit `index.yaml` and add an entry under `spec.entries` with `name`, `version`, `description`, and `source: <template-name>/template.yaml`.
   - Keep versions semver-like and increment when you change templates.

3. **Test locally with the CLI**
   ```bash
   # Point to your local index (no fetch from GitHub needed)
   promptarena templates list --index ../promptkit-templates/index.yaml

   # Render using the repo/template shorthand (fills cache then render)
   promptarena templates fetch --index ../promptkit-templates/index.yaml --template your-template --version 1.0.0
   promptarena templates render --index ../promptkit-templates/index.yaml --template your-template --version 1.0.0 --values values.example.yaml --out ./out
   ```
   - Alternatively, add a repo shortname: `promptarena templates repo add --name local --url ../promptkit-templates/index.yaml` then use `local/your-template`.

4. **Validate with Go tests (optional but recommended)**
   ```bash
   GOCACHE=.cache/go-build go test ./tools/arena/templates ./tools/arena/cmd/promptarena
   ```
   - This exercises template loading, repo resolution, and CLI flows.

5. **Documentation and examples**
   - Include a `README.md` in your template directory describing generated files and how to run.
   - Provide a `values.example.yaml` showing the expected variables.

## Submission checklist
- [ ] Template builds locally (`templates render` succeeds).
- [ ] `index.yaml` updated with correct version and source path.
- [ ] Variables and provider options use current provider names (`openai`, `claude`, `gemini`, `mock`).
- [ ] README and example values included.
- [ ] Tests pass locally (optional but encouraged).

When ready, open a PR against `promptkit-templates` with the new template and index update.***
