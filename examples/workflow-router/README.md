# workflow-router

Demonstrates `control: agent` workflow states for multi-transition turns.

The triage agent reads the user's first message and decides whether to send
them to billing or technical support — but the routing requires two
transitions: `triage → routed` and then `routed → billing` (or
`routed → technical`). Without an agent-controlled state, the second
transition would have to wait for an extra user message that never comes.

`routed` is declared `control: agent`, so when the agent fires the first
transition the state machine commits eagerly and the agent keeps the turn,
firing the destination transition in the same pipeline tool loop.

## Workflow

```
                     ┌──────────────┐
        Routed       │   routed     │   ToBilling
       ───────────►  │ control:agent│  ───────────► billing (terminal)
                     │              │
                     │              │   ToTechnical
                     │              │  ───────────► technical (terminal)
                     └──────────────┘
                            ▲
        ┌────────┐  Routed  │
   ──►  │ triage │ ─────────┘
        └────────┘
```

## Run

```bash
cd examples/workflow-router
promptarena run --ci --formats html,json
```

Both scenarios produce two transitions per opening pipeline turn:

- `route-to-billing`: routed (eager) + billing (deferred)
- `route-to-technical`: routed (eager) + technical (deferred)

State-aware assertions verify that `state_is` and `transitioned_to` see
the committed destination, and `workflow_transition_order` confirms both
transitions fired in the expected sequence inside a single turn.
