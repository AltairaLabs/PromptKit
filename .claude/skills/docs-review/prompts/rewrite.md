You rewrite one {product} docs page so that it follows docs/STYLE.md (below, in
full) and states only verified facts. Edit the file in place with the Edit
tool. Apply block moves to their target pages, creating a target page in the
right quadrant directory if it does not exist.

Inputs: stage 0 findings (mechanical rule hits), stage 1 classification,
stage 3 verification.

Hard rules:
1. Facts come from stage 3 only. For each claim:
   - `true`: keep it.
   - `false`: state the fact in `correct`, with its exact values. `correct`
     fixes the fact, not the wording: write it in the page's register. On a
     reference page that means a positive statement (a table that lists the
     real fields, "defaults to X"), never "X is not a field", "there is no
     default", rationale or `file.go:line` citations. If the page no longer
     makes the false claim, the correction needs no sentence at all.
   - `dead`: remove it and every mention of it.
   - `unverified`: leave the original text unchanged.
2. Fix every stage 0 finding.
3. Apply every stage 1 block action. A moved block gets a one-line link in its
   place, e.g. "See [Providers](/<product>/reference/providers/) for every
   option."
4. Follow the page skeleton and length budget of the page's quadrant.
5. Diagrams:
   - Convert ASCII art, D2 and diagram images to Mermaid.
   - Add the diagrams stage 1 marked "missing"; remove the ones it marked
     "redundant".
   - Use no style or classDef, keep to about 12 nodes, and use the real names.
   - Give each diagram a one-sentence lead-in.
6. Generated pages are not edited. If the page is the output of a generator
   (see the skill's "This repository" section), edit its source instead.
7. Keep headings stable where you can. Other pages link to their anchors.
8. Keep frontmatter `title` and `description`. Rewrite `description` in the
   same voice.

Return JSON only:
{ "page": "<path>", "wordsBefore": 0, "wordsAfter": 0,
  "moved": [ { "from": "<path>#<heading>", "to": "<path>" } ],
  "created": [ "<path>" ], "diagrams": [ "<line: added|converted|removed>" ],
  "unverifiedKept": [ <claim ids> ] }
