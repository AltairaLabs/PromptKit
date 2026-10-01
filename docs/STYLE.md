# PromptKit documentation style

This guide governs every page under `docs/src/content/docs/`. It is for people
and agents writing or reviewing those pages. The `/docs-review` skill applies
it, and `scripts/check-docs-voice.sh` checks the parts a script can check.

## Reader

The reader is a Go developer building an LLM application with the
PromptKit SDK or runtime. They know Go modules, `context`, and YAML. They
want the function, the option, and what the runtime does with it. Write for
that person. Do not explain Go basics, and do not sell.

## Universal rules

These apply to every page, whatever its quadrant.

- **Truth.** Every checkable claim matches the code on `main`: type, function, option, default, field, enum, provider name, event,
  metric, behaviour.
  Diagrams and code or YAML examples are claims too.
- **No implementation status.** If it is in the docs, it is implemented. Do
  not write "planned", "not yet", "Phase 2", "later" or "currently" as a
  hedge. An option that nothing reads is not documented; report it for
  removal instead.
- **No project history.** No issue numbers or issue links, open or closed.
  No "used to", "was renamed", "previously" or "since vN". Versioned changes belong in
  `CHANGELOG.md` and the upgrade notes.
- **Present tense, active voice.** "The pipeline retries the call", not "the
  call will be retried".
- **Second person.** Address the reader as "you", never as "the user" or
  "the developer".
- **One claim per sentence.** Keep paragraphs short. Cut any sentence that
  restates the one before it.

## Banned patterns

Rules marked *(checked)* are enforced by `scripts/check-docs-voice.sh` from
`scripts/docs-voice-banned.txt`. The rest are enforced in review.

### Status words *(checked: `status`)*

- Before: "Only `openai` is currently supported; others are coming soon."
- After: "Supported providers: `openai`."

### Faux candour and intent-signalling *(checked: `faux-candour`)*

"Honestly", "to be clear", "deliberately", "by design", "on purpose", "kept
honest". State the behaviour. If the reason matters, give the reason.

- Before: "The runtime deliberately does not retry on `400`."
- After: "The runtime does not retry on `400`."

### Signposting *(checked: `signposting`)*

"Which is exactly", "that is the point", "this is why", "the trap", "the
catch".

- Before: "…the tool result is appended to history — which is exactly what
  the next turn reads."
- After: "The next turn reads the tool result from history."

### Filler *(checked: `filler`)*

"Simply", "just", "note that", "it's worth noting", "importantly",
"crucially", "essentially", "basically". Delete the word; if the sentence
then needs emphasis, it belongs in an aside.

- Before: "Simply pass the option to `Open()`."
- After: "Pass the option to `Open()`."

### Apologies and hedges *(checked: `apology`)*

"Unfortunately", "sadly", "sorry". State the limit as a fact.

### History *(checked: `history`, `issue-ref`)*

- Before: "The flag used to be called `--out`."
- After: (delete; describe what happens now)
- Before: "Streaming falls back to polling (see #1596)."
- After: "Streaming falls back to polling."

A page cannot know when a ticket closes, and nothing in a docs build notices.
Describe the limitation itself. If closing an issue would make a documented
statement untrue, updating that page belongs in the issue's acceptance
criteria: links run ticket to docs, never the reverse.

### Positioning *(checked: `positioning`)*

"Best-in-class", "seamless", "effortless", "unlike other tools". Naming a
competitor to say it is worse ages badly and is often wrong on the details.
Naming one as a vocabulary reference ("if you know X's terminology") is
fine, and so is an honest "when to use something else" section.

### Internal content *(checked: `internal`)*

Links a reader cannot follow (`docs/local-backlog/`, a proposal section
number), working notes ("spike", "deferred to", "Day 3 of"), and FIXMEs.

### Aphorisms and punchline closers

A closing line that restates the paragraph as a maxim.

- Before: "Nothing was slow; nothing was being asked."
- After: (delete; the preceding sentence states the fact)

### Contrast framing

"Not X — it is Y" and "X, not Y" used as a rhetorical device. Say what it is.
Name what it is not only when readers commonly assume it.

- Before: "A pack is not a config file — it is a compiled artifact."
- After: "A pack is a compiled artifact: prompts, tools and validators in one
  JSON file."

### Em-dashes *(checked: more than one per paragraph)*

Use a full stop, a colon or parentheses instead.

### Bold lead-ins

Do not start every paragraph or list item with a bold phrase. Bold marks a
term the reader must not miss, at most once or twice per section.

### Rhetorical questions

Replace them with the statement they imply. FAQ headings phrased as the
reader's question are fine.

## Formatting

- Use Starlight asides (`:::note`, `:::caution`) only for information whose
  absence causes harm.
- List fields, options and flags in tables.
- Give every code block a language.
- Go snippets compile against the current API, with their imports. Elide
  with a comment (`// ...`), never with invalid syntax.

## Diagrams

Mermaid is the only diagram format. Do not use ASCII box-art, D2 or image
files for diagrams.

| Quadrant | Diagrams |
|---|---|
| Explanation | architecture, data flow, sequence. The main home for diagrams. |
| Reference | state diagrams for states and transitions only |
| How-to | only when the reader must see a topology to carry out the steps |
| Tutorial | at most one: what you will have built |

- Nodes carry the real names: types, packages, binaries, stages.
- Keep to about 12 nodes; split anything larger.
- Put a one-sentence lead-in before the diagram, saying what it shows.
- Do not use `style` or `classDef` overrides. The site theme handles light and
  dark mode.
- A diagram must agree with the prose and the code.

`docs/check-mermaid.mjs` parses every diagram. Mermaid renders in the
browser, so the site build does not catch a broken diagram.

## Quadrants

The docs follow [Diátaxis](https://diataxis.fr/). A page's directory is its quadrant: `runtime/` and `sdk/` each have
`tutorials/`, `how-to/`, `reference/` and `explanation/`. `concepts/` and
`architecture/` are explanation; top-level `reference/` is reference. Each page serves
one quadrant. Content that serves another quadrant moves to a page in that
quadrant, and the source page links to it.

### Tutorials

- **Serves:** learning.
- **Register:** "we"; one guaranteed path; a visible result after each step.
- **Never contains:** alternatives, explanation beyond one sentence, option
  tables.
- **Skeleton:** what you will build (optional diagram) → prerequisites →
  steps, each ending in a visible result → next steps.
- **Length:** at most 2,500 words.

### How-to guides

- **Serves:** a goal the reader already has.
- **Register:** imperative; assumes competence; conditional branches allowed
  ("If you use Vertex AI, …").
- **Never contains:** teaching, background, full option lists (link to the
  reference page instead).
- **Skeleton:** one-sentence goal → prerequisites → numbered steps → verify →
  troubleshooting (optional) → related pages.
- **Length:** at most 1,200 words.

### Reference

- **Serves:** information.
- **Register:** austere and descriptive; the structure mirrors the API,
  schema or flag set.
- **Never contains:** instructions, rationale, opinion.
- **Skeleton:** one-paragraph purpose → package or schema, and where it is
  used → one table per type or object in source order (name, type, default,
  description) → examples → related
  pages.
- **Length:** proportional to the number of fields; no prose beyond what each
  field needs.

### Explanation

- **Serves:** understanding.
- **Register:** discursive. Covers the why, the trade-offs and the
  alternatives, and may hold a view.
- **Never contains:** numbered steps, option tables.
- **Skeleton:** the question it answers → how it works (diagram) → trade-offs
  and alternatives → related pages.
- **Length:** at most 2,000 words.

## Exemplars

None yet. The pages of the first `/docs-review` batch become the exemplars,
one per quadrant; list them here when it merges.

## Keeping pages true

A page reviewed by `/docs-review` carries a `verified` record in its
frontmatter: the commit its claims were checked against, and the source
files they were checked against. A sidecar in `docs/provenance/` maps each
claim to its evidence. `scripts/docs-provenance.py` writes both; do not edit
them by hand.

- `scripts/docs-stale.sh` lists pages whose facts may have changed since
  their verified commit: a change touched a line one of their claims cites,
  or added lines to a public-surface file the page owns.
- `scripts/docs-coverage.sh` lists public-surface files (the globs in
  `scripts/docs-surface.txt`) that no page cites.
  `scripts/docs-coverage-ignore.txt` holds the deliberate exceptions.

Both run warn-only on every PR (in the Docs Checks job summary) and weekly
(the `docs-freshness` issue). When a PR changes a file a page cites, update
the page in the same PR. `/docs-review --stale` fixes what the weekly issue
lists, re-checking only the claims whose sources changed.

## What is checked mechanically

- `scripts/docs-voice-banned.txt` is the source of truth for the checked
  rules above. `scripts/check-docs-voice.sh` applies it, together with the
  em-dash, ASCII-diagram and unterminated-fence rules. Findings fail in
  directories listed in `scripts/docs-voice-enforced.txt` and warn
  elsewhere.
- `docs/check-mermaid.mjs` parses every Mermaid diagram.
