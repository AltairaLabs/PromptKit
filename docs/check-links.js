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

console.log(`🚀 Starting preview server on port ${port}...`);
const server = spawn(
  'npx',
  ['astro', 'preview', '--port', String(port)],
  {
    detached: true,
    stdio: ['ignore', 'pipe', 'pipe'],
  },
);

// Wait until the preview server reports it's listening on the expected
// port — if astro falls back to a different port (because ours is taken),
// fail fast rather than silently crawling the wrong content. Astro emits
// its "ready" line with ANSI color codes in both CI and local shells, so
// strip those before matching.
const stripAnsi = (s) => s.replace(/\x1B\[[0-9;]*[A-Za-z]/g, '');
const readyRegex = new RegExp(`http://localhost:${port}/`);
const fallbackRegex = /http:\/\/localhost:(\d+)\//;
let ready = false;
let fallbackPort;

const readyPromise = new Promise((resolve, reject) => {
  const onData = (chunk) => {
    const raw = chunk.toString();
    process.stdout.write(raw);
    const text = stripAnsi(raw);
    if (readyRegex.test(text)) {
      ready = true;
      resolve();
      return;
    }
    const match = text.match(fallbackRegex);
    if (match && match[1] !== String(port)) {
      fallbackPort = match[1];
      reject(
        new Error(
          `astro preview fell back to port ${fallbackPort} because ${port} is busy. ` +
          `Set CHECK_LINKS_PORT to a free port or stop the process using ${port}.`,
        ),
      );
    }
  };
  server.stdout.on('data', onData);
  server.stderr.on('data', onData);
  once(server, 'exit').then(() => {
    if (!ready) {
      reject(new Error('astro preview exited before becoming ready'));
    }
  });
  // Safety net — reject if the server never reports ready.
  setTimeout(() => {
    if (!ready) {
      reject(new Error(`astro preview did not become ready on port ${port} within 30s`));
    }
  }, 30_000);
});

try {
  await readyPromise;
  console.log(`🔍 Checking ${includeExternal ? 'all' : 'internal-only'} links...\n`);

  const checker = new LinkChecker();
  const result = await checker.check({
    path: baseURL,
    recurse: true,
    timeout: 10000,
    // linksToSkip takes an array of regex strings; matching URLs are not
    // fetched. Skip any absolute URL that isn't on localhost or the
    // production docs domain — relative links have already been resolved
    // to localhost by the time linkinator checks them, so this correctly
    // skips only real third-party URLs.
    linksToSkip: includeExternal
      ? undefined
      : ['^https?://(?!localhost|127\\.0\\.0\\.1|promptkit\\.altairalabs\\.ai).+'],
  });

  console.log(`\n📊 Total links checked: ${result.links.length}\n`);

  const brokenLinks = result.links.filter((link) => link.state === 'BROKEN');

  if (brokenLinks.length === 0) {
    console.log('✅ No broken links found!');
    process.exit(0);
  }

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
      url.startsWith('/') ||
      url.startsWith('https://promptkit.altairalabs.ai')
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

  process.exit(1);
} catch (error) {
  console.error('Error:', error.message);
  process.exit(1);
} finally {
  try {
    process.kill(-server.pid);
  } catch (e) {
    // Ignore
  }
}
