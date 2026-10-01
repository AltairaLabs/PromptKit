#!/usr/bin/env node
// Parses every ```mermaid block under the docs tree. Mermaid renders in the
// browser (public/mermaid-init.js), so the site build never sees a broken
// diagram; this is the only place one is caught before it ships.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

// Mermaid sanitises labels with DOMPurify, which needs a window at import
// time. Install one first, then load mermaid.
const { window } = new JSDOM('');
globalThis.window = window;
globalThis.document = window.document;
const { default: mermaid } = await import('mermaid');

const FENCE = /^(\s*)(```|~~~)\s*(\S*)/;

export function extractMermaid(text) {
  const blocks = [];
  let open = null; // { marker, indent, mermaid, line, lines }
  text.split('\n').forEach((raw, i) => {
    const m = raw.match(FENCE);
    if (open) {
      if (m && m[2] === open.marker) {
        if (open.mermaid) blocks.push({ line: open.line, source: open.lines.join('\n') });
        open = null;
      } else if (open.mermaid) {
        open.lines.push(raw.slice(Math.min(open.indent, raw.length - raw.trimStart().length)));
      }
      return;
    }
    if (m) open = { marker: m[2], indent: m[1].length, mermaid: m[3] === 'mermaid', line: i + 1, lines: [] };
  });
  return blocks;
}

export async function checkFile(path) {
  const errors = [];
  for (const b of extractMermaid(readFileSync(path, 'utf8'))) {
    try {
      await mermaid.parse(b.source);
    } catch (e) {
      errors.push(`${path}:${b.line}: mermaid: ${String(e.message).split('\n')[0]}`);
    }
  }
  return errors;
}

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else if (/\.mdx?$/.test(name)) yield p;
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const root = process.argv[2] ?? fileURLToPath(new URL('./src/content/docs', import.meta.url));
  let failed = 0;
  let count = 0;
  for (const f of walk(root)) {
    count += extractMermaid(readFileSync(f, 'utf8')).length;
    for (const err of await checkFile(f)) {
      console.log(relative(process.cwd(), err.split(':')[0]) + err.slice(err.indexOf(':')));
      failed++;
    }
  }
  console.log(`${count} mermaid diagrams checked, ${failed} broken`);
  process.exit(failed ? 1 : 0);
}
