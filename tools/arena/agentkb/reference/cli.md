# CLI Reference

Generated from the command tree. Run `promptarena <cmd> --help` for full detail.

## promptarena agent-brief

Write the authoring brief (AGENTS.md + .claude/skills) into a project

## promptarena chat

Chat interactively with an agent defined in an Arena config

Flags: `--config`, `--echo-guard`, `--mock-config`, `--mock-provider`, `--voice-output-voice`, `--voice-stt`, `--voice`

## promptarena config-inspect

Inspect and validate prompt arena configuration

Flags: `--config`, `--format`, `--section`, `--short`, `--stats`, `--verbose`

## promptarena debug

Debug configuration and prompt loading

Flags: `--config`

## promptarena deploy

Deploy the arena pack to a target environment

- `promptarena deploy adapter` — Manage deploy adapter binaries
- `promptarena deploy apply` — Apply the deployment
- `promptarena deploy config` — Manage the deploy configuration
- `promptarena deploy destroy` — Tear down a deployment
- `promptarena deploy import` — Import a pre-existing resource into deployment state
- `promptarena deploy plan` — Preview the deployment plan
- `promptarena deploy refresh` — Refresh local state from the live environment
- `promptarena deploy status` — Show deployment status

## promptarena examples

Discover example PromptArena kits

- `promptarena examples list` — List example kits from the embedded catalog
- `promptarena examples show` — Print an example kit's files

## promptarena explain

Explain a PromptArena authoring concept

Flags: `--list`

## promptarena export

Export arena config as a compiled PromptPack

Flags: `--config`, `--id`, `--output`

## promptarena generate

Generate scenario files from session data

Flags: `--dedup`, `--filter-eval-type`, `--filter-passed`, `--from-recordings`, `--output`, `--pack`, `--source`

## promptarena init

Initialize a new Arena test project

Flags: `--no-agent`, `--no-env`, `--no-git`, `--output`, `--provider`, `--quick`, `--repo-config`, `--template-cache`, `--template-index`, `--template`, `--verbose`

## promptarena mcp

Run an MCP server exposing PromptArena authoring knowledge over stdio

## promptarena mocks

Manage mock responses for PromptArena

- `promptarena mocks generate` — Generate mock provider responses from Arena JSON results

## promptarena prompt-debug

Debug and test prompt generation

Flags: `--config`, `--context`, `--domain`, `--json`, `--list`, `--persona`, `--region`, `--scenario`, `--show-meta`, `--show-prompt`, `--show-stats`, `--task-type`, `--user`, `--verbose`

## promptarena render

Generate HTML report from existing results

Flags: `--output`

## promptarena run

Run conversation simulations

Flags: `--audio-monitor`, `--audio-rate`, `--ci`, `--concurrency`, `--config`, `--eval-types`, `--eval`, `--format`, `--formats`, `--html-file`, `--html`, `--junit-file`, `--markdown-file`, `--max-tokens`, `--mock-config`, `--mock-provider`, `--out`, `--override-provider`, `--provider`, `--region`, `--roles`, `--scenario`, `--seed`, `--selfplay`, `--simple`, `--skip-pack-evals`, `--temperature`, `--verbose`

## promptarena schema

Print the embedded JSON schema for a config type

Flags: `--list`

## promptarena serve

Start the Arena web UI with live run streaming

Flags: `--audio-monitor`, `--audio-rate`, `--mock-config`, `--mock-provider`, `--open`, `--port`

## promptarena skill

Manage AgentSkills.io skills

- `promptarena skill install` — Install a skill from Git or local path
- `promptarena skill list` — List installed skills
- `promptarena skill remove` — Remove an installed skill

## promptarena templates

Manage PromptArena templates (list, fetch, render)

- `promptarena templates fetch` — Fetch a template from an index into cache
- `promptarena templates list` — List templates from an index
- `promptarena templates render` — Render a cached template to an output directory (dry-run only)
- `promptarena templates repo` — Manage template repositories
- `promptarena templates update` — Update all templates from an index into cache

## promptarena validate

Validate configuration files against JSON schemas

Flags: `--json`, `--schema-only`, `--type`, `--verbose`

## promptarena view

Browse and view past Arena test results

