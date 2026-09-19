#!/usr/bin/env node
import { LinkChecker } from 'linkinator';
import { spawn } from 'child_process';
import { once } from 'events';
import { readdir, readFile } from 'fs/promises';
import { join, relative } from 'path';

// Walk the built site and verify every in-page fragment resolves to an
// element id on the page it points at. Reads dist/ from disk: no server, and
// independent of what the crawler chose to follow.
async function checkFragments(distDir) {
  const files = [];
  const walk = async (dir) => {
    for (const e of await readdir(dir, { withFileTypes: true })) {
      const full = join(dir, e.name);
      if (e.isDirectory()) await walk(full);
      else if (e.name.endsWith('.html')) files.push(full);
    }
  };
  await walk(distDir);

  // Path a link resolves to -> the file serving it.
  const fileFor = (href, fromFile) => {
    let path = href.split('?')[0];
    if (path === '') return fromFile; // same-page "#frag"
    if (!path.startsWith('/')) return null; // relative: rare here, skip
    if (path.endsWith('/')) return join(distDir, path, 'index.html');
    if (path.endsWith('.html')) return join(distDir, path);
    return join(distDir, path + '/index.html');
  };

  const idCache = new Map();
  const idsOf = async (file) => {
    if (idCache.has(file)) return idCache.get(file);
    let ids = null; // null = page missing; a broken LINK, reported elsewhere
    try {
      const html = await readFile(file, 'utf8');
      // Both spellings of an anchor target. The generated Go reference pages
      // come out of gomarkdoc as <a name="Symbol"></a>, not id="", so an
      // id-only scan reports every symbol link on those pages as missing —
      // 5691 false positives, which is indistinguishable from no check at all.
      // name="" is read only off <a>, so form fields and <meta> do not
      // manufacture ids that would mask a real miss.
      ids = new Set([
        ...[...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]),
        ...[...html.matchAll(/<a\s[^>]*name="([^"]+)"/g)].map((m) => m[1]),
      ]);
    } catch {
      // leave null
    }
    idCache.set(file, ids);
    return ids;
  };

  const bad = [];
  const seen = new Set();
  for (const file of files) {
    const html = await readFile(file, 'utf8');
    for (const m of html.matchAll(/href="([^"#]*)#([^"]+)"/g)) {
      const [, path, rawFragment] = m;
      if (path.startsWith('http') || path.startsWith('mailto:')) continue;
      const fragment = decodeURIComponent(rawFragment);
      // Starlight emits its own in-page machinery; skip the obvious ones.
      if (fragment === '' || fragment === '_top') continue;
      const target = fileFor(path, file);
      if (!target) continue;
      const ids = await idsOf(target);
      if (ids === null) continue; // missing page, not a missing fragment
      if (ids.has(fragment)) continue;
      const key = `${target}#${fragment}`;
      if (seen.has(key)) continue;
      seen.add(key);
      bad.push({
        href: `${path}#${fragment}`,
        fragment,
        from: relative(distDir, file),
      });
    }
  }
  return bad;
}

// In CI we only want to catch broken *internal* links — the kind we can
// actually fix. External URLs flake for reasons out of our control (rate
// limiting, temporary outages, auth gates) and turn the docs job red for
// no useful signal. Set CHECK_LINKS_INCLUDE_EXTERNAL=1 to get the full
// crawl locally.
const includeExternal = process.env.CHECK_LINKS_INCLUDE_EXTERNAL === '1';

// Explicitly pick a port and pass it to astro preview so we always crawl
// OUR built dist and never accidentally crawl whatever happens to be
// listening on astro's default port (e.g. another dev server the user
// has running locally). CI gets a clean host so this is a no-op there,
// but it makes local runs robust.
const port = Number(process.env.CHECK_LINKS_PORT ?? '4321');
const baseURL = `http://localhost:${port}/`;

// Readiness is decided by polling the port, not by parsing astro's output.
// Recent astro versions run `preview` as a detached daemon: the spawned
// process exits immediately after handing off, and prints a JSON log line
// rather than the plain "ready" line this used to match. Both broke the old
// detection — the run failed with "astro preview exited before becoming
// ready" even on a free port. An HTTP probe is independent of log format and
// of whether the server daemonized.
const probe = async () => {
  try {
    const res = await fetch(baseURL, { method: 'HEAD' });
    return res.ok || res.status === 404; // serving, even if / is not a page
  } catch {
    return false;
  }
};

const run = (args) =>
  new Promise((resolve) => {
    const p = spawn('npx', ['astro', ...args], { stdio: 'ignore' });
    once(p, 'exit').then(() => resolve());
  });

// A daemon left over from an earlier run holds the port and is not ours to
// crawl — its dist may be stale. Stop it before checking whether anything
// else is listening.
await run(['preview', 'stop']);

if (await probe()) {
  console.error(
    `Error: something is already serving ${baseURL}. ` +
    `Stop it, or set CHECK_LINKS_PORT to a free port.`,
  );
  process.exit(1);
}

console.log(`🚀 Starting preview server on port ${port}...`);
const server = spawn(
  'npx',
  ['astro', 'preview', '--port', String(port)],
  {
    detached: true,
    stdio: ['ignore', 'pipe', 'pipe'],
  },
);
server.stdout.on('data', (c) => process.stdout.write(c));
server.stderr.on('data', (c) => process.stdout.write(c));

// If astro falls back to another port because ours is taken, nothing ever
// answers ours and this times out — which is the same outcome the old
// fallback check produced, without having to recognize the message.
const readyPromise = (async () => {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (await probe()) return;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`astro preview did not become ready on port ${port} within 30s`);
})();

// Set rather than exited on: process.exit() skips the finally block below,
// which is how a previous run could leave the preview daemon holding the port
// and fail the next one before it started.
let exitCode = 1;

try {
  await readyPromise;
  console.log(`🔍 Checking ${includeExternal ? 'all' : 'internal-only'} links...\n`);

  const checker = new LinkChecker();
  const result = await checker.check({
    path: baseURL,
    recurse: true,
    timeout: 10000,
    // linksToSkip takes an array of regex strings; matching URLs are not
    // fetched. Skip any absolute URL that isn't on localhost — relative
    // links have already been resolved to localhost by the time linkinator
    // checks them, so this correctly skips real third-party URLs as well as
    // the production docs domain. Production URLs are deliberately excluded
    // because Starlight emits <link rel="canonical"> and og:url tags that
    // point at the prod URL of every page; for any new page in a PR that
    // URL 404s until the PR is merged and deployed, which would otherwise
    // make the link check fail on every PR that adds a new page.
    linksToSkip: includeExternal
      ? undefined
      : ['^https?://(?!localhost|127\\.0\\.0\\.1).+'],
  });

  console.log(`\n📊 Total links checked: ${result.links.length}\n`);

  // linkinator resolves a URL and stops at the page: it strips the fragment
  // before recording a link, so a link to #section-that-does-not-exist passes
  // as OK and silently drops the reader at the top of the page. (Verified —
  // of 202 links on one page, zero reach result.links with a '#' in them, so
  // a check written over result.links cannot fail.) That is how nine links to
  // #composition-checks / #assertion-wrapper stayed green while the target
  // headings had ids like "composition-checks-composition-checks": the
  // `{#custom-id}` syntax is not supported here, so it was slugified INTO the
  // id it was meant to set, and rendered as visible text in the heading.
  //
  // So read the built HTML directly rather than linkinator's link list.
  const badFragments = await checkFragments('./dist');

  if (badFragments.length > 0) {
    console.log(`🔗 MISSING FRAGMENTS (${badFragments.length}):\n`);
    for (const { href, fragment, from } of badFragments) {
      console.log(`  [#${fragment}] ${href}`);
      console.log(`      linked from ${from}\n`);
    }
  } else {
    console.log('✅ Every in-page fragment resolves to an element.\n');
  }

  const brokenLinks = result.links.filter((link) => link.state === 'BROKEN');

  if (brokenLinks.length === 0) {
    console.log('✅ No broken links found!');
    exitCode = badFragments.length > 0 ? 1 : 0;
  } else {

  // Group by broken URL
  const linksByUrl = new Map();
  for (const link of brokenLinks) {
    if (!linksByUrl.has(link.url)) {
      linksByUrl.set(link.url, []);
    }
    linksByUrl.get(link.url).push(link.parent);
  }

  // Separate internal and external
  const internal = [];
  const external = [];

  for (const [url, parents] of linksByUrl) {
    const entry = { url, parents: [...new Set(parents)] };
    if (
      url.startsWith(baseURL) ||
      url.startsWith('http://localhost:') ||
      url.startsWith('/')
    ) {
      internal.push(entry);
    } else {
      external.push(entry);
    }
  }

  console.log(`❌ Found ${brokenLinks.length} broken links (${linksByUrl.size} unique URLs)\n`);

  if (internal.length > 0) {
    console.log(`🔴 INTERNAL BROKEN LINKS (${internal.length}):\n`);
    for (const { url, parents } of internal) {
      console.log(`  [404] ${url}`);
      console.log(`      Found on ${parents.length} page(s):`);
      parents.slice(0, 3).forEach((p) => console.log(`        - ${p}`));
      if (parents.length > 3) {
        console.log(`        ... and ${parents.length - 3} more`);
      }
      console.log('');
    }
  }

  if (external.length > 0) {
    console.log(`\n🌐 EXTERNAL BROKEN LINKS (${external.length}):\n`);
    for (const { url, parents } of external) {
      console.log(`  [404] ${url}`);
      console.log(`      Found on ${parents.length} page(s):`);
      parents.slice(0, 3).forEach((p) => console.log(`        - ${p}`));
      if (parents.length > 3) {
        console.log(`        ... and ${parents.length - 3} more`);
      }
      console.log('');
    }
  }

    exitCode = 1;
  }
} catch (error) {
  console.error('Error:', error.message);
  exitCode = 1;
} finally {
  // Both forms: `preview stop` reaches the daemon, the group kill reaches an
  // older astro that stayed in the foreground.
  await run(['preview', 'stop']);
  try {
    process.kill(-server.pid);
  } catch {
    // Already gone.
  }
}

process.exit(exitCode);
