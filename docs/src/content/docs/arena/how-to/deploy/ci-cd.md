---
title: 'Deploy: CI/CD Integration'
---

## Goal

Automate deployments with GitHub Actions.

## Prerequisites

- Deploy configuration in arena.yaml
- A compiled `.pack.json` committed to your repository (or compiled in CI)
- Cloud provider credentials configured as GitHub secrets

## GitHub Actions Workflow

### Basic Deploy on Push

Deploy to production when code is pushed to the main branch:

```yaml
# .github/workflows/deploy.yml
name: Deploy
on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install PromptKit
        run: |
          go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest
          go install github.com/AltairaLabs/PromptKit/tools/packc@latest

      - name: Install adapter
        run: promptarena deploy adapter install agentcore

      - name: Compile pack
        run: packc compile --config arena.yaml --output app.pack.json --id my-app

      - name: Deploy
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_REGION: us-west-2
        run: promptarena deploy --env production
```

### Plan on Pull Request

Preview changes on pull requests without applying:

```yaml
# .github/workflows/deploy-plan.yml
name: Deploy Plan
on:
  pull_request:
    branches: [main]

jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install PromptKit
        run: |
          go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest
          go install github.com/AltairaLabs/PromptKit/tools/packc@latest

      - name: Install adapter
        run: promptarena deploy adapter install agentcore

      - name: Compile pack
        run: packc compile --config arena.yaml --output app.pack.json --id my-app

      - name: Plan deployment
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_REGION: us-west-2
        run: promptarena deploy plan --env production
```

### Multi-Environment Pipeline

Deploy to staging first, then production after approval:

```yaml
# .github/workflows/deploy-pipeline.yml
name: Deploy Pipeline
on:
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install tools
        run: |
          go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest
          go install github.com/AltairaLabs/PromptKit/tools/packc@latest

      - name: Compile pack
        run: packc compile --config arena.yaml --output app.pack.json --id my-app

      - name: Upload pack
        uses: actions/upload-artifact@v4
        with:
          name: pack
          path: app.pack.json

  deploy-staging:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install tools
        run: |
          go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest

      - name: Install adapter
        run: promptarena deploy adapter install agentcore

      - name: Download pack
        uses: actions/download-artifact@v4
        with:
          name: pack

      - name: Deploy to staging
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
        run: promptarena deploy --env staging --pack app.pack.json

  deploy-production:
    needs: deploy-staging
    runs-on: ubuntu-latest
    environment: production
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install tools
        run: |
          go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest

      - name: Install adapter
        run: promptarena deploy adapter install agentcore

      - name: Download pack
        uses: actions/download-artifact@v4
        with:
          name: pack

      - name: Deploy to production
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
        run: promptarena deploy --env production --pack app.pack.json
```

The `environment: production` setting enables GitHub's environment protection rules, requiring manual approval before the production deployment runs.

## Makefile Integration

Add deploy targets to your Makefile:

```makefile
.PHONY: deploy-plan deploy deploy-status deploy-destroy

deploy-plan:
	promptarena deploy plan --env $(ENV)

deploy:
	promptarena deploy --env $(ENV)

deploy-status:
	promptarena deploy status --env $(ENV)

deploy-destroy:
	promptarena deploy destroy --env $(ENV)
```

Usage:

```bash
make deploy-plan ENV=staging
make deploy ENV=staging
make deploy-status ENV=staging
```

## State Management in CI

The deploy state file (`.promptarena/deploy.state`) is stored locally. In CI environments, state is typically not persisted between runs.

### Options for CI State

**Option 1: Commit state to the repository**

```yaml
- name: Deploy
  run: promptarena deploy --env production

- name: Commit state
  run: |
    git add .promptarena/deploy.state
    git commit -m "Update deploy state" || true
    git push
```

**Option 2: Use artifact storage**

```yaml
- name: Download prior state
  uses: actions/download-artifact@v4
  with:
    name: deploy-state
  continue-on-error: true

- name: Deploy
  run: promptarena deploy --env production

- name: Upload state
  uses: actions/upload-artifact@v4
  with:
    name: deploy-state
    path: .promptarena/deploy.state
```

## Troubleshooting

### Adapter not available in CI

Install the adapter in your CI workflow before deploying:

```yaml
- name: Install adapter
  run: promptarena deploy adapter install agentcore
```

### Credentials not available

Ensure secrets are configured in your GitHub repository settings and referenced correctly:

```yaml
env:
  AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
  AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
```

### State file missing between runs

See the state management options above. Without persisting state, each CI run treats the deployment as new.

## See Also

- [Plan and Apply](plan-and-apply) — Manual deployment workflows
- [Configure Deploy](configure) — arena.yaml setup
- [State Management](../../explanation/deploy/state-management) — Understanding deploy state
