#!/usr/bin/env node
import { LinkChecker } from 'linkinator';
import { spawn } from 'child_process';
import { once } from 'events';

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

  const brokenLinks = result.links.filter((link) => link.state === 'BROKEN');

  if (brokenLinks.length === 0) {
    console.log('✅ No broken links found!');
    exitCode = 0;
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
