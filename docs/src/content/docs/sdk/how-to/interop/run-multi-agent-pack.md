---
title: Run a multi-agent pack in process
description: Open a pack's agents section with OpenMultiAgent so an entry agent delegates to member agents through tool calls, all inside one process.
sidebar:
  order: 3
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/packspec/types.go
    - sdk/agent_resolver.go
    - sdk/capability_a2a.go
    - sdk/local_agent_executor.go
    - sdk/local_agent_executor_impl.go
    - sdk/multi_agent.go
    - sdk/multi_agent_open.go
    - sdk/options.go
---

Run an entry agent and its member agents from one pack, with agent-to-agent calls routed in process instead of over HTTP.

## Prerequisites

- A pack with an `agents` section that names an `entry` and its `members`. Each member key matches a prompt in the pack, or names a workflow `state`.
- Provider credentials for the agents' models.

## Steps

### 1. Declare the agents in the pack

```json
{
  "agents": {
    "entry": "triage",
    "members": {
      "triage":  { "description": "Routes and answers billing questions." },
      "refunds": { "description": "Issues refunds." }
    }
  }
}
```

A member with a `state` key runs the pack's workflow from that state. Any other member runs the prompt named by its key.

List the members the entry agent may call in the entry agent's prompt `tools`, by member key (`refunds`) or qualified name (`a2a__refunds`). Only the entry agent's calls route to members in process:

```json
{
  "prompts": {
    "triage": { "tools": ["refunds"] }
  }
}
```

### 2. Open the session

```go
session, err := sdk.OpenMultiAgent("./support.pack.json",
	sdk.WithModel("gpt-4o"),
)
if err != nil {
	return err
}
defer session.Close()
```

`OpenMultiAgent` applies your options to every agent, entry and members alike. It fails when the pack has no `agents` section, no members, or no `entry`. It opens the members first and the entry last, and closes any member it opened if a later open fails.

### 3. Send to the entry agent

```go
resp, err := session.Send(ctx, "I was double charged on my last invoice")
if err != nil {
	return err
}
fmt.Println(resp.Text())
```

The entry agent sees each member listed in its prompt `tools` as an agent tool with a required `query` argument. When it calls one, the SDK sends the query to that member and returns the member's reply as the tool result.

## Verify

Send a message that needs a member. The reply comes from the entry agent and reflects the member's answer.

## Inspect the session

| Method | Returns |
|---|---|
| `Send(ctx, message, opts...)` | The entry agent's `*Response`. |
| `Entry()` | The entry `Agent`. |
| `Members()` | A copy of the member map, without the entry. |
| `Close()` | Closes the entry and every member. Close errors are joined with `errors.Join`. |

`Agent` is the interface `Send(ctx, message, opts...)` and `Close()`. A single-prompt agent is a `*Conversation`. A state-backed agent is a `*WorkflowConversation`.

## Route member calls yourself

`LocalAgentExecutor` implements `tools.Executor` under the name `a2a`, so it takes the calls that would otherwise go to remote A2A agents. A call carries `{"query": "..."}` and the executor returns `{"response": "<member reply>"}`.

The executor finds the member by the bare name at the end of the qualified tool name, so `a2a__refunds` reaches `refunds`. An unknown name returns an error to the model. A call whose context has no deadline gets a five-minute timeout.

To route calls yourself, pass `sdk.WithA2AToolExecutor(sdk.NewLocalAgentExecutor(members))` when you open a conversation, where `members` is a `map[string]sdk.Agent`.

## Related

- [Connect A2A agents](/sdk/how-to/interop/connect-a2a-agents/)
- [A2A](/concepts/a2a/)
- [Governance](/concepts/governance/)
