---
name: docs-review
description: Review published documentation for accuracy, stale claims, internal content and competitive positioning. Use before a release, when docs have drifted, or when auditing what a repo publishes. Verifies commands and identifiers against the real binary rather than reading prose.
---

# Documentation review

Prose review finds typos. This finds **claims that are false** — commands that
error, flags that were renamed, limitations that were fixed months ago, and
internal notes published to strangers.

Work in this order. Step 1 changes what the rest of the review even looks at, and
skipping it is how you review the wrong files.

This skill is shared across the PromptArena family of repositories. Everything
below "This repository" is identical in each; a diff between copies is drift.

## 1. Establish the publication surface

Before reading anything, find out what gets published and from where. Three
categories, handled differently:

- **Authored** — edit directly.
- **Generated from repo sources** — edit the *source*, not the output.
- **Fetched from another repository** — cannot be fixed here at all. An edit
  survives until the next build, then silently reverts. Fix it in the owning
  repo and open a PR there.

Confirm before editing:

```bash
git check-ignore -v <path>   # ignored ⇒ generated or fetched, not authored
git ls-files <dir> | wc -l   # 0 tracked files ⇒ same conclusion
```

### This repository

A library, so most "commands" in docs are Go snippets rather than CLI calls —
check them against the current API, and against `sdk/` examples that compile.

- **Go doc comments are published.** `docs/scripts/gen-reference.sh` and
  `gen-sdk-reference.sh` turn them into reference pages under
  `docs/src/content/api/` and `docs/src/content/sdk/`, all gitignored and rebuilt
  at deploy time. A doc comment is public documentation: a URL written inline in
  one rendered as a live link and 404'd the docs build.
- CI's Reference Drift check runs `check-reference.sh` / `check-sdk-reference.sh`.
  A comment change that alters the reference must be regenerated, not hand-edited.
- `docs/local-backlog/` is gitignored and local-only. Never cite it, or a section
  number within it, from anything published.
- The docs workflow only runs for changes under `docs/`, so a doc-comment change
  elsewhere can break the site without any PR showing it.

## 2. Verify every command against the binary

The highest-yield check, and entirely mechanical. Docs claim flags exist; the
binary is the authority.

```bash
# Real flags
<cli> <subcommand> --help | grep -oE '\-\-[a-z-]+' | sort -u > /tmp/real.txt

# Flags the docs use
grep -rhoE '<cli> <subcommand> [^|]*' docs/ examples/*/README.md 2>/dev/null \
  | grep -oE '\-\-[a-z-]+' | sort -u > /tmp/doc.txt

comm -13 /tmp/real.txt /tmp/doc.txt     # documented but non-existent
```

Confirm each hit really fails, rather than being a root-level or renamed flag:

```bash
<cli> <subcommand> --suspect-flag 2>&1 | grep -oE "unknown flag.*"
```

Check the **binary name itself**. Docs outlive renames: promptarena's quickstarts
told readers to run `arena`, which no binary provides — the CLI is `promptarena`,
so the first command of two quickstarts could not be followed.

Do the same for any identifier with a registry behind it — assertion types, eval
types, provider names. Parse the YAML rather than grepping `type:`, which also
matches provider and schema fields.

## 3. Check every issue reference

**The rule across these repos is: no issue links in documentation at all.** Not
open ones either. A page cannot know when a ticket closes, and nothing in a docs
build notices — four voice caveats pointed at issues closed two months earlier,
still telling readers that shipped features were missing.

```bash
grep -rEo "github.com/[^)]+/issues/[0-9]+" docs/ examples/*/README.md 2>/dev/null
```

Check state before deciding what to write:

```bash
gh issue view <n> --repo <owner/repo> --json state,stateReason,title
```

Replace with the limitation itself. **Verify the limitation before restating
it** — closed-as-completed does not mean shipped. The acoustic-echo issues were
closed months ago, but AEC is still absent from the released runtime, so both
"it's coming" and "it works" would have been wrong.

The counterpart belongs on the ticket: if closing an issue would make a
documented statement untrue, updating that page goes in its acceptance criteria.
Links run ticket → docs, never the reverse.

## 4. Scan for internal content

Anything a reader cannot follow, or should not see:

```bash
grep -rniE "local-backlog|roadmap|Day [0-9]|spike|proposal §|deferred to|v1 ?/ ?v2|\
Issue #[0-9]+|TODO|FIXME|not yet implemented|planned for|future release" \
  docs/ examples/ 2>/dev/null | grep -viE "v1alpha1|alphanumeric"
```

That exclusion matters — `v1alpha1` matches `alpha` and buries real hits in false
positives.

Distinguish two things that look alike:

- *"`graph` and `composite` are accepted by the schema but not yet implemented
  (they behave as `keyword`)"* — **keep**. An accurate limitation depending on no
  ticket.
- *"Issue #216: Recording adapter system (future)"* — **remove**. Internal
  tracking and a promise.

## 5. Scan for positioning

```bash
grep -rniE "best.in.class|industry.leading|world.class|revolutionary|\
unlike other|competitors?|vs\.? [A-Z]|seamless|effortless|just works" docs/ examples/ 2>/dev/null
```

Naming a competitor to say it is worse ages badly, invites argument, and is often
wrong on the details — one comparison here claimed a competitor was
Python-specific and single-templates-only, neither true. Naming one as a
*vocabulary* reference ("if you already work in X's terminology") is helpful and
should stay, as should an honest "what to use something else for" section.

## 6. Verify documented behaviour by running it

Prose describing behaviour drifts silently. Run the thing and count.

Beware double-counting when two layers log the same event: the PromptKit runtime
and PromptArena both emit `workflow state transition` with identical fields, so
raw log counts are twice the real number.

## 7. Build and link-check

```bash
cd docs && npm run build
CHECK_LINKS_PORT=4455 npm run check-links   # slow: checks external URLs
```

If a preview server is already running on the default port, the checker binds
elsewhere and fails to start — hence the explicit port. `npx astro preview stop`
clears a stale daemon.

## Traps this review actually hit

Each produced a wrong conclusion before being caught:

- **`find -maxdepth 2`** missed files nested three deep, leading to "these don't
  exist" — and three real files were overwritten before the mistake surfaced.
  Check `git status` after any bulk write: `M` where you expected `A` means you
  clobbered something.
- **`grep -h`** strips filenames, so a following `grep -v <path>` filters nothing.
  Stale generated pages then read as live source hits.
- **Assuming a missing file is a bug.** A skill directory absent from the repo
  turned out to be written by `init` into *new* projects; verified by running it
  and listing the output. The page was accurate and needed no change.
- **Fixing before reproducing.** Every correction should be confirmed by running
  the command, checking the issue state, or counting real output first.

## Reporting

Separate what you verified from what you inferred, and say which claims you could
not check. A finding is "this command errors, here is the output" — not "this
looks outdated".
