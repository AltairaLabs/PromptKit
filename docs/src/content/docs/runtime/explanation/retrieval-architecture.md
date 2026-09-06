---
title: Retrieval Architecture
sidebar:
  order: 6
---
Why retrieval has two shapes in an agent, what each costs, and why PromptKit ships interfaces for it rather than an implementation.

## Two shapes

An agent that needs knowledge it was not given can obtain it in one of two ways, and the difference is *who decides*.

**Model-initiated.** The model reads the question, concludes it needs something, and calls a tool. Retrieval happens mid-turn, as part of the tool loop.

**Pipeline-initiated.** The pipeline retrieves before the model runs and puts the result in the system prompt. The model never chooses; the content is simply there.

Both end with retrieved text in the model's context. Everything else about them differs.

## What each costs

Model-initiated retrieval spends a round trip. The model must generate a tool call, the tool must execute, and the model must be called again with the result — so the floor is two provider calls where ambient injection needs one. In exchange, the query is the model's own reading of what it needs, which is usually better than a heuristic derived from the raw user turn, and it retrieves nothing when nothing is needed.

Its failure mode is silence. If the model does not think to call the tool, no retrieval happens and the answer is ungrounded with no signal that anything was missed. Prompt wording carries real weight here, and smaller models skip the call more often.

Pipeline-initiated retrieval costs nothing extra in provider calls and cannot be skipped: every turn is grounded whether the model would have asked or not. What it gives up is judgment. The query is built from the conversation by fixed logic, so it retrieves the same way for a question it handles well and one it handles badly.

Two consequences are easy to miss:

**It changes the system prompt every turn.** Providers cache prompt prefixes, and a system prompt whose middle section is rewritten per turn cannot be reused from cache. On a long system prompt this is a real cost — measure it before assuming ambient injection is the cheaper option.

**A bad retrieval is worse than none.** Injected content arrives with the model's own instructions, carrying the authority of the system prompt, and the model has no way to know it was chosen by a heuristic rather than by relevance. It will use an irrelevant document rather than ignore it. A retriever that returns its best guess when nothing matches is actively harmful; returning empty is correct.

## Choosing

Grounding that should apply to every answer — a product catalog, a policy set, the documentation an agent speaks for — belongs in the pipeline. The model should not have to remember to consult the thing it exists to speak about, and the round trip you save is spent on every single turn.

Knowledge that is occasionally relevant, or expensive to fetch, belongs behind a tool. So does anything where the query needs interpretation the raw conversation does not supply.

Facts about the user specifically are a third case, and the deciding question is whether the agent should *write* as well as read. Writing is necessarily model-initiated: only the model knows that something in the conversation was worth keeping. An agent that remembers is therefore holding a tool already, and reading through the same tool keeps one story about where those facts live.

They compose, and in a mature agent usually do: ambient grounding in the corpus the agent speaks for, tools for the subject's own history.

## Keep the sources apart

A tempting shortcut is to point ambient retrieval at the same store the memory tools write to. It reliably confuses the resulting agent.

The model then encounters the same rows twice — once injected without explanation, once by asking — with no way to tell that they are the same rows. Worse, the two paths usually apply different filters: a host that enforces consent or tenancy inside its `Retriever` gets no such enforcement on the tool path, because the tool goes to the store directly. What looks like one policy is two, and only one of them is written down.

Separate sources keep the responsibilities legible: the corpus grounds answers, the store remembers the subject, and each has one filter.

## Why PromptKit ships no retriever

`memory.Retriever` is an interface with no production implementation in PromptKit, and that is deliberate rather than unfinished.

Relevance is a property of a corpus. What counts as the right document depends on how your content is chunked, what embedding model indexed it, whether the domain wants recall or precision, and how the answer must be cited. A default would be wrong for most corpora while looking authoritative, and hosts would build around it before discovering that.

What PromptKit owns instead is the seam: when retrieval runs, what it is handed, and how its output reaches the prompt. `corpus.Retriever` exists to make that seam testable and to give a working example — term overlap over a fixed document set, adequate for development and honest about being nothing more.

The same reasoning applies to `memory.Store` and `memory.Extractor`. Persistence, ranking, and deciding what is worth remembering are all domain judgments; the interfaces are the contract, and platform layers implement them.

## Where this lives in the pipeline

Ambient retrieval runs as a stage between prompt assembly and template rendering. That position is forced: the stage writes a template variable, and the template stage is the single render point, so retrieval must complete before the render or the variable is invisible. It also means the retriever sees message text before variable substitution has been applied to it.

Ambient retrieval is therefore unavailable in duplex (realtime) sessions, where there is no per-turn input boundary to retrieve against. The SDK refuses the configuration rather than starting a session that can never reply. Model-initiated retrieval has no such constraint — the tool loop runs inside a turn either way.

Tool-based retrieval runs inside the provider stage's tool loop, which is why it can happen several times in one turn and why its results appear in the transcript as tool messages rather than in the system prompt.

## See also

- [Memory and Grounding](/sdk/how-to/conversations/use-memory/) — configuring both paths through the SDK
- [Pipeline Architecture](/runtime/explanation/pipeline-architecture/) — stage ordering and the render point
- [Memory reference](/runtime/reference/memory/) — the interfaces described here
