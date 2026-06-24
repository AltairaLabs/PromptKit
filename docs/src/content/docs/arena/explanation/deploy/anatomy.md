---
title: 'Deploy: Anatomy of a Deployment'
---

**How a pack and a deploy config become a running agent — independent of which adapter you use.**

---

## Two inputs

Every deployment combines exactly two things:

| Input | What it is | Who interprets it |
|-------|------------|-------------------|
| The **pack** (`.pack.json`) | The portable artifact compiled from your `arena.yaml` — the same one you tested in Arena. | The adapter (and ultimately the runtime). |
| The **deploy config** (`deploy.config` in `arena.yaml`) | Environment binding for one target. | **Only the adapter** — the CLI passes it through verbatim. |

The CLI itself understands neither the contents of the pack nor the contents of `deploy.config`. It compiles the pack, hands both to the adapter as opaque JSON, and the adapter translates them into provider-native resources.

```d2
direction: right

pack: .pack.json {
  label: ".pack.json\n(what the agent IS)"
}
config: deploy.config {
  label: "deploy.config\n(how it RUNS here)"
}
adapter: Adapter {
  label: "Adapter\n(translator)"
}
target: Target {
  label: "Provider resources\n(CRDs, runtimes, …)"
}

pack -> adapter
config -> adapter
adapter -> target
```

---

## The pack is the portable contract

The pack is **what the agent is**, expressed independently of where it runs. It is environment-agnostic on purpose: the same pack can be tested locally, deployed to staging, and promoted to production unchanged. It carries:

- **Prompts and system templates** — the agent's behavior.
- **Tool contracts** — the *schemas* the model sees (name, description, input/output shape). This is what the model is allowed to call.
- **Guardrails and validators** — the behavioral envelope enforced at runtime.
- **Tool policies** — allow/blocklists that constrain tool use.
- **Eval definitions** — what "good" means for this agent.
- **Agent topology** — for multi-agent packs, the set of agents and how they relate.
- **Identity and version** — the pack ID and version.

If a fact about your agent is true *no matter where it runs*, it belongs in the pack.

---

## The deploy config is the environment binding

`deploy.config` supplies what is **inherently specific to one environment** — and therefore deliberately *not* in the pack:

- Which concrete model/provider actually serves the agent.
- How tools actually execute here (real endpoints, handlers, infrastructure).
- Credentials, authentication, scaling, and other infra knobs.
- Per-environment overrides (dev / staging / production).

This block is **opaque to the CLI and defined entirely by the adapter**. An AWS adapter's config looks nothing like a Kubernetes adapter's. The exact fields it accepts, and the exact resources it produces, are documented by **each adapter** — not here. This page is only about the *shape* of the relationship, not any adapter's specifics.

---

## The principle: declare abstractly, bind concretely

The line between pack and config follows one rule: **the pack declares a capability abstractly; the deployment binds it to concrete infrastructure.** Two recurring forms of this are worth naming, because they explain most "where does X go?" questions:

**Contract vs handler.** A pack carries a tool's *contract* — the schema the model can call against. It does **not** carry how that tool executes in production. The execution binding (an HTTP endpoint, an MCP server, a gRPC target, …) is environment-specific and is supplied at deploy time by the adapter. So a tool the model can *call* and a tool the platform can *run* are two halves bound from two different inputs.

**Role vs instance.** A pack is written against a *role* — "this agent needs an LLM," "this step needs an embedder" — not against a specific hosted model. The deployment binds those roles to concrete providers. The pack stays portable across providers precisely because it never names one.

### Corollary: test-time config is not deploy-time config

The providers you wire into `arena.yaml` to *run scenarios* are **test fixtures**. They exist so Arena can exercise the pack locally. They do **not** deploy. Production bindings come from `deploy.config` (or are derived by the adapter against the target platform). Sharing a name between a test provider and a deploy binding is coincidence, not coupling.

---

## What's portable vs bound — at a glance

| Concern | Lives in | Why |
|---------|----------|-----|
| Prompts, system templates | Pack | The agent's behavior — portable. |
| Tool contracts (schemas) | Pack | What the model may call — portable. |
| Guardrails / validators | Pack | Behavioral envelope — portable. |
| Tool policies (allow/block) | Pack | Behavioral envelope — portable. |
| Eval definitions | Pack | What "good" means — portable. |
| Agent topology | Pack | Structure — portable. |
| Model / provider binding | Deploy config (adapter) | Which infra serves it — environment-specific. |
| Tool execution (handlers/endpoints) | Deploy config (adapter) | How tools run *here* — environment-specific. |
| Credentials, auth, scaling | Deploy config (adapter) | Infrastructure — environment-specific. |

---

## See Also

- [Deploy: Overview](/arena/explanation/deploy/overview/) — what Deploy is and how the pieces connect
- [Adapter Architecture](/arena/explanation/deploy/adapter-architecture/) — the plugin/JSON-RPC mechanics that carry these two inputs to the adapter
- [Configure Deploy](/arena/how-to/deploy/configure/) — adding the `deploy` section to `arena.yaml`
- Your adapter's own reference docs — for the exact config fields it accepts and the exact resources it creates
