# AGENTS.md Brief Eval Harness (Arm C)

Dogfood: a brief-equipped coding agent authors PromptArena kits in a sandbox;
PromptArena scores them. **Live only — never run in CI** (real Claude + Docker).

## Run

```bash
export ANTHROPIC_API_KEY=...
make -C ../.. build-agents-brief-sandbox      # bakes promptarena+packc+scripts
promptarena run -c configs/arm-c-brief.arena.yaml --ci --format html
open out/report.html
```

## What's scored

Five gates as `conversation_assertions` against `/workspace/kit` after the agent
finishes: schema-valid (`promptarena validate`), compiles (`packc`), scenarios
run green (`promptarena run --ci`), no unused files, and an `llm_judge_session`
faithfulness gate. A non-gating `idiom-traps.sh` metric reports idiom-trap and
adequacy counts.

## Wiring check (no API spend, needs Docker)

See `configs/arm-c-mock.arena.yaml` — a `type: mock` provider scripts the tool
sequence; gates execute for real against a known-good kit (and a known-bad kit
that must fail).
