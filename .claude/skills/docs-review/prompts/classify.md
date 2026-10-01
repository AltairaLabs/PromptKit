You label one {product} docs page by Diátaxis quadrant. You do not rewrite anything.

The page's quadrant is fixed by its directory: {quadrant}. The quadrant rules
and diagram rules from docs/STYLE.md follow the page.

Split the page into blocks: each heading section, and inside a section each
paragraph, list, table, code block, aside, diagram or image. Label at that
level, not per section: a long reference page has dozens of blocks, and one
explanatory paragraph inside a field section is exactly what you are looking
for. For each block
decide which quadrant its CONTENT serves:
- tutorial: teaches by doing one guaranteed path
- how-to: steps toward a goal the reader already has
- reference: describes fields, flags, routes, states — austere facts
- explanation: why, how it works, trade-offs

Return JSON only:
{
  "page": "<path>",
  "quadrant": "<quadrant>",
  "blocks": [
    { "id": 1, "startLine": 12, "endLine": 30, "heading": "<nearest heading>",
      "serves": "<quadrant>", "action": "keep|move|link|delete",
      "target": "<existing page path or new page path, for move/link>",
      "why": "<one sentence>" }
  ],
  "diagrams": [
    { "startLine": 40, "status": "ok|redundant|wrong-format|missing",
      "note": "<for missing: what it would show; for wrong-format: ascii, d2 or image>" }
  ]
}

"move" means the block belongs on another page, and this page keeps a link.
Before proposing one, check the content's owner:
- A how-to keeps the commands and config its steps need; only full option
  lists leave it.
- A reference page keeps its one-paragraph purpose and a usage example.
- A comparison of alternatives is explanation (trade-offs), not reference.
"link" means the target already covers it, so replace the block with a link.
"delete" is only for content that duplicates text on this same page.
To find existing target pages, list the quadrant directories named in the
skill's "This repository" section and match by topic. Do not invent page paths when a matching page exists, and never propose a
new page for a single block: keep it or link the closest existing page.
