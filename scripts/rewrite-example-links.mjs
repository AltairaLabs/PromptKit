#!/usr/bin/env node
// rewrite-example-links.mjs
//
// Reads markdown from stdin and rewrites relative links to absolute GitHub
// URLs, then writes the result to stdout. Used by prepare-examples-docs.sh
// when copying example READMEs into the Starlight content collection.
//
// Example READMEs use repo-relative links like `[foo](prompts/foo.yaml)` or
// `[bar](../../docs/guides/bar.md)` which are valid when viewed on GitHub but
// resolve to 404s when the README is rendered as an Astro page (Astro doesn't
// serve raw YAML, and the `../../docs/...` paths don't map to Starlight
// routes). Rewriting to absolute github.com/AltairaLabs/PromptKit URLs makes
// the links work in both contexts and keeps the linkinator check green —
// GitHub URLs are skipped by default (internal-only mode) and they point at
// real files (or get a clean 404 from GitHub) rather than 404ing within our
// own docs host.
//
// Usage: node rewrite-example-links.mjs <source-dir>
//   <source-dir> is the README's directory relative to the repo root,
//   e.g. "examples/document-analysis" or "sdk/examples/hello".

import path from 'node:path';

const sourceDir = process.argv[2];
if (!sourceDir) {
  console.error('Usage: rewrite-example-links.mjs <source-dir>');
  process.exit(1);
}

const GITHUB_BLOB_PREFIX = 'https://github.com/AltairaLabs/PromptKit/blob/main/';

const chunks = [];
process.stdin.on('data', (chunk) => chunks.push(chunk));
process.stdin.on('end', () => {
  const input = Buffer.concat(chunks).toString('utf8');

  // Match markdown link syntax: [text](url) and ![alt](url). The URL is
  // captured without parentheses — nested parens in URLs are rare in READMEs
  // and we prefer a simple regex over a full parser.
  const output = input.replace(
    /(!?\[[^\]]*\]\()([^)\s]+)(\))/g,
    (match, openBracket, url, closeBracket) => {
      // Leave absolute URLs, fragments, mailto, and site-absolute paths alone.
      if (/^(https?:|mailto:|tel:|#|\/)/i.test(url)) {
        return match;
      }

      // Split off any fragment so path.normalize doesn't touch it.
      const hashIdx = url.indexOf('#');
      const rawPath = hashIdx >= 0 ? url.slice(0, hashIdx) : url;
      const fragment = hashIdx >= 0 ? url.slice(hashIdx) : '';

      // Resolve the relative path against the README's source directory.
      // path.posix is used so path separators stay `/` on every platform.
      const resolved = path.posix.normalize(path.posix.join(sourceDir, rawPath));

      // If normalization escapes above the repo root (starts with `..`),
      // leave the link alone — we can't represent it as a GitHub URL.
      if (resolved.startsWith('..')) {
        return match;
      }

      return `${openBracket}${GITHUB_BLOB_PREFIX}${resolved}${fragment}${closeBracket}`;
    },
  );

  process.stdout.write(output);
});
