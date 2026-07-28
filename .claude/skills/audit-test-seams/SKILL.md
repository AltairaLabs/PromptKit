---
name: audit-test-seams
description: Use when auditing a subsystem's tests for untested component handoffs and assertions that cannot fail - finds seams where two units each pass their own tests but the contract between them is unverified
---

# Auditing Test Seams

## Overview

Four defects shipped through a suite with 88% coverage and a 2:1 test-to-production
line ratio. Each sat at a *seam* — a point where one component hands off to
another and the contract is written down in neither. Two failure modes let
them through:

1. **Untested seams.** A contract between two components, written down in
   neither. Each side's tests assert against that side's *assumption* about
   the other, so both suites pass while the handoff is broken.
2. **Assertions that cannot fail.** Tests checking only `NoError`/`Nil`/`NotNil`,
   which pass whether or not the code produced the right answer — while
   counting fully toward the coverage gate.

Coverage does not detect either. This audit does. `tools/seamscan` (the
`seamscan` command) finds candidates for both; this skill is the judgment that
turns a candidate into a finding.

**Announce at start:** "I'm using audit-test-seams to audit `<target>`."

## Scope

Audit **one subsystem at a time** — a package tree someone is actively working
in, e.g. `runtime/hooks`. Per-package is the default and useful unit. A
whole-repo run produces a report nobody reads (well over a hundred packages
across the workspace modules) and a whole-repo pattern is rejected outright —
see Step 1.

## Step 1: Run the analyzer

Run from the **repo root** (not from inside `tools/seamscan` — the module is
part of this repo's `go.work`, so `go run ./tools/seamscan` resolves
correctly without a directory change):

```bash
# weak-assertions takes a plain directory, walked recursively already —
# do NOT add a "/..." suffix, it will look for a literal directory of that name.
go run ./tools/seamscan weak-assertions --text <target>

# seams uses go/packages patterns and DOES need the "/..." suffix to recurse;
# a bare directory scans only that one package, non-recursively.
go run ./tools/seamscan seams --text <target>/...
```

Both commands emit JSON by default; `--text` is for a human reading the
output directly (`file:line`, kind, subject, one per line). `seams` also
accepts `--min-fields` (default 5, ignore structs with fewer exported fields)
and `--min-dropped` (default 3, ignore literals dropping fewer fields) if a
subsystem's own struct conventions call for retuning them.

A whole-repo pattern (`./...` from the repo root) **deliberately errors**
rather than silently reporting zero — `seamscan` treats "no packages could be
loaded" as a hard failure, not an empty result. Scope the pattern to a real
subsystem.

Findings are **candidates**. The analyzer decides nothing; every candidate
below is judged, not accepted as-is.

## Known limitations — read before trusting a clean scan

A scan with zero findings is not an all-clear. State these to whoever reads
the results:

1. **The counts are floors, not ground truth.** Whole-tree `weak-assertions`
   count and any `seams` count are lower bounds on what exists, not exhaustive
   totals.
2. **Lossy-rebuild misses method-call-sourced values.** It detects a struct
   literal that reads an existing *field* but drops most of the type's other
   fields. It does **not** see a value that comes from a method call on a
   collaborator (`state.accumulatedContent()`) rather than a field read —
   `sdk/streaming.go:444` (`&types.Message{Role, Content}`, 2 of 13 fields,
   dropping `FinishReason`) is a live, undetected instance of exactly the
   defect class this signature is named for. Recovering it needs call-graph
   analysis or a per-site allowlist. The converse also holds: `x.Field.Method()`
   **does** qualify (the receiver chain reads a field), so some findings are
   method-call results that may be unrelated to the field read.
3. **Lossy-rebuild is literal-only.** It cannot see fields assigned after
   construction (`m := T{}; m.X = …`); its `not set in literal:` list can name
   fields that are, in fact, set later in the same function.
4. **Weak-assertions suppresses locally-defined `assert*`/`must*` helpers.**
   This is a deliberate bias toward under-reporting — a guard that fires on
   legitimate helper-wrapped tests gets disabled by whoever it annoys — so a
   weak assertion hidden behind a local helper is invisible to the scan.
5. **Weak-assertions misses a closure that a subtest-hosting parent defines
   and invokes itself** (as opposed to delegating to a `t.Run` subtest). Its
   weak assertion is silently unflagged.
6. **Weak-assertions has a known false-positive shape.** `NotNil`/`Nil` on a
   field whose zero value is genuinely reachable (i.e. the check really can
   fail) gets flagged anyway, because the heuristic is blanket: any bare
   `NoError`/`Nil`/`NotNil`/…-`f` call counts as "proves nothing," without
   checking whether the specific call is actually unfalsifiable. Measured
   rate on the last 20 merged PRs: 1 in 20. Expect to dismiss some candidates
   for exactly this reason — see Step 3.

## Step 2: Judge each seam candidate

For each candidate, answer in order. Stop at the first "no."

1. **Is there a real contract here?** Two components exchanging data with an
   expectation neither states. A partial literal for a builder is not a
   contract; a message rebuilt at a module boundary is.
2. **Could the two sides disagree while both pass their own tests?** This is
   the actual test. If side A cannot be wrong about side B, there is no seam.
3. **Does any test exercise both sides with the real collaborator?** This is
   the handoff-coverage check, and it needs a concrete rule or it is
   unfalsifiable:

   > A test counts as covering the seam if it imports both packages, or
   > constructs both concrete types, **and neither side is substituted**.
   > "Substituted" means: the test constructs a type from a package whose
   > path or name contains `mock`, `fake`, `stub`, or `testutil`, **and**
   > that type stands in for one of the two sides of the seam under test.

   A mock of something that is *not* one of the two sides is irrelevant — a
   seam test for adapter↔handler may legitimately use a mock *provider*,
   because the provider isn't either side of that seam.

   This heuristic will misjudge a hand-rolled fake that follows no naming
   convention. Treat its answer as a prompt to go look at the test, not as a
   verdict by itself.

Only candidates reaching step 3 with "no test" are findings.

## Step 3: Judge each weak-assertion candidate

For each, name the **mutation** the test would catch. Break the
implementation in your head — return the zero value, skip the side effect,
invert the condition — and ask whether the test notices.

- Notices → not weak, dismiss the candidate. This is where limitation 6 above
  shows up in practice: a `require.Nil(t, ValidatorsToHooks(...))` call looks
  like the boilerplate the tool is built to catch, but if the function can
  legitimately return a non-nil empty slice instead of nil, the assertion is
  real and the candidate is a false positive.
- Does not notice → finding. Either strengthen the assertion or delete the
  test.

Watch specifically for an assertion that is *structurally* unable to fail:
`assert.NotEqual(X, field)` where nothing ever sets `field`, or
`require.NoError` on a call that cannot error. Also watch for a test that
duplicates a compile-time interface assertion already made in production code
(`var _ SomeInterface = (*Impl)(nil)`) — it is not wrong, just dead weight;
recommend deleting it rather than strengthening it.

## Step 4: Report

For each finding give: the two sides (for a seam) or the test (for a weak
assertion), what the contract or assertion should be, and the concrete
failure a test would catch. Rank by blast radius — a seam on a safety path
(guardrails, auth, budget enforcement) outranks one on a debug helper.

Do not write the tests as part of the audit. The audit produces a work list;
writing tests is separate work with its own review.

## Anti-patterns

| Temptation | Why it is wrong |
|---|---|
| "Coverage is 90%, we are fine" | Coverage measures executed lines, not verified behavior. All the weak tests are coverage-positive. |
| Adding a unit test for each side | That is what already failed. The seam needs one test running both sides for real. |
| Mocking the collaborator "to isolate" | The mock encodes your assumption about it. When the assumption is wrong the test agrees with you. |
| Fixing candidates without judging them | Most candidates are not defects. Unjudged fixes add noise and churn. |
| Deleting weak tests wholesale | Some are weak but load-bearing (smoke tests). Strengthen where the behavior matters. |
| Treating a clean scan as an all-clear | See Known limitations above — the scan is a floor, not a certificate. |

## Cost of the cleanup

Removing assertion-free tests **lowers** the coverage number: they execute
real lines, and the seam tests replacing them are far fewer. Expect a dip,
and agree it is acceptable before starting — otherwise the coverage gate
blocks the cleanup and the fix is to backfill exactly the junk you removed.

## Non-goals

- **No generated tests.** The audit produces a work list of findings; a human
  or agent writes the tests as separate, separately-reviewed work.
- **No score, grade, or percentage.** A number becomes a target, and targets
  get gamed — that is the failure mode of the coverage gate this audit exists
  to supplement. Report findings, not a metric.
- **No gating.** This audit does not block a build or a merge. (Weak, brand
  new assertions can separately be caught by running `make check-weak-assertions`
  locally — a different, narrower mechanism; see
  `scripts/check-weak-assertions.sh`. It is local opt-in tooling only: it is
  not run in pre-commit, it is not wired into
  `.github/workflows/ci.yml`, and it is not this skill.)
