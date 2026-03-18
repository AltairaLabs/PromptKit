#!/usr/bin/env node

/**
 * Fetches adapter documentation from external repos at build time and
 * maps them into the arena/ documentation sections.
 *
 * Supports multiple adapters — each defined in the ADAPTERS array.
 *
 * Usage:
 *   node scripts/fetch-adapter-docs.mjs [--ref <branch|tag>]
 *
 * Environment:
 *   SKIP_ADAPTER_DOCS=1   Skip fetching (for fast local dev)
 */

import { execSync } from "child_process";
import fs from "fs/promises";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const DOCS_ROOT = path.join(__dirname, "..");
const ARENA_DIR = path.join(DOCS_ROOT, "src/content/docs/arena");

const SOURCE_PREFIX = "docs/src/content/docs/";

// Skip upstream index pages — arena already has section indexes.
const SKIP_FILES = new Set([
  "tutorials/index.md",
  "how-to/index.md",
  "reference/index.md",
  "explanation/index.md",
]);

// Mapping from old /deploy/ absolute links to their new arena paths (shared across adapters).
const DEPLOY_LINK_MAP = {
  "/deploy/": "/arena/explanation/deploy/overview/",
  "/deploy/tutorials/01-first-deployment/":
    "/arena/tutorials/deploy/first-deployment/",
  "/deploy/tutorials/02-multi-environment/":
    "/arena/tutorials/deploy/multi-environment/",
  "/deploy/how-to/install-adapters/":
    "/arena/how-to/deploy/install-adapters/",
  "/deploy/how-to/configure-deploy/": "/arena/how-to/deploy/configure/",
  "/deploy/how-to/plan-and-apply/": "/arena/how-to/deploy/plan-and-apply/",
  "/deploy/how-to/ci-cd-integration/": "/arena/how-to/deploy/ci-cd/",
  "/deploy/reference/cli-commands/": "/arena/reference/deploy/cli-commands/",
  "/deploy/reference/adapter-sdk/": "/arena/reference/deploy/adapter-sdk/",
  "/deploy/reference/protocol/": "/arena/reference/deploy/protocol/",
  "/deploy/explanation/adapter-architecture/":
    "/arena/explanation/deploy/adapter-architecture/",
  "/deploy/explanation/state-management/":
    "/arena/explanation/deploy/state-management/",
};

// Absolute link prefixes that belong to parent PromptKit docs — leave as-is.
const PARENT_PREFIXES = ["/packc/", "/sdk/", "/arena/", "/runtime/"];

// Sections that exist inside adapter docs (used to detect internal absolute links).
const ADAPTER_SECTIONS = [
  "how-to",
  "reference",
  "explanation",
  "tutorials",
];

// ---------------------------------------------------------------------------
// Adapter definitions
// ---------------------------------------------------------------------------

/**
 * Build adapter config for a standard adapter repo.
 *
 * Convention: each adapter has the same upstream doc structure and maps to
 * arena/{section}/deploy/{adapterName}/{file}.
 */
function buildAdapterConfig(name, repo, extraFiles = {}) {
  // Standard files every adapter has.
  const standardFiles = {
    "index.md": { target: `explanation/deploy/${name}/overview.md`, order: 50 },
    "tutorials/01-first-deployment.md": {
      target: `tutorials/deploy/${name}/first-deployment.md`,
      order: 50,
    },
    "tutorials/02-multi-agent.md": {
      target: `tutorials/deploy/${name}/multi-agent.md`,
      order: 50,
    },
    "how-to/configure.md": {
      target: `how-to/deploy/${name}/configure.md`,
      order: 50,
    },
    "how-to/dry-run.md": {
      target: `how-to/deploy/${name}/dry-run.md`,
      order: 50,
    },
    "reference/configuration.md": {
      target: `reference/deploy/${name}/configuration.md`,
      order: 50,
    },
    "reference/resource-types.md": {
      target: `reference/deploy/${name}/resource-types.md`,
      order: 50,
    },
    "explanation/resource-lifecycle.md": {
      target: `explanation/deploy/${name}/resource-lifecycle.md`,
      order: 50,
    },
    "explanation/security.md": {
      target: `explanation/deploy/${name}/security.md`,
      order: 50,
    },
  };

  // Standard slug mappings.
  const slugMap = {
    "01-first-deployment": "first-deployment",
    "02-multi-agent": "multi-agent",
    configure: "configure",
    "dry-run": "dry-run",
    configuration: "configuration",
    "resource-types": "resource-types",
    "resource-lifecycle": "resource-lifecycle",
    security: "security",
  };

  // Build absolute internal link map from the file map.
  const absoluteInternalMap = {};
  for (const [src, mapping] of Object.entries({ ...standardFiles, ...extraFiles })) {
    // Derive the upstream absolute path from the source filename.
    const upstreamSlug = src.replace(/\.md$/, "").replace(/^index$/, "");
    if (upstreamSlug) {
      const upstreamPath = `/${upstreamSlug}/`;
      const arenaPath = `/arena/${mapping.target.replace(/\.md$/, "")}/`;
      absoluteInternalMap[upstreamPath] = arenaPath;
    }
  }

  return {
    name,
    repo,
    fileMap: { ...standardFiles, ...extraFiles },
    slugMap: { ...slugMap, ...Object.fromEntries(
      Object.entries(extraFiles).map(([src]) => {
        const slug = path.basename(src, ".md");
        return [slug, slug];
      }),
    )},
    absoluteInternalMap,
  };
}

const ADAPTERS = [
  buildAdapterConfig(
    "agentcore",
    "AltairaLabs/promptarena-deploy-agentcore",
    {
      // Extra files specific to agentcore.
      "how-to/tagging.md": {
        target: "how-to/deploy/agentcore/tagging.md",
        order: 50,
      },
      "how-to/observability.md": {
        target: "how-to/deploy/agentcore/observability.md",
        order: 50,
      },
      "reference/environment-variables.md": {
        target: "reference/deploy/agentcore/env-vars.md",
        order: 50,
      },
      "reference/runtime-protocols.md": {
        target: "reference/deploy/agentcore/runtime-protocols.md",
        order: 50,
      },
    },
  ),
  buildAdapterConfig(
    "omnia",
    "AltairaLabs/PromptArena-deploy-omnia",
    {
      // Extra files specific to omnia.
      "how-to/labels.md": {
        target: "how-to/deploy/omnia/labels.md",
        order: 51,
      },
    },
  ),
];

// Add agentcore-specific deploy link mapping.
DEPLOY_LINK_MAP["/deploy/adapters/agentcore/"] =
  "/arena/explanation/deploy/agentcore/overview/";
DEPLOY_LINK_MAP["/deploy/adapters/omnia/"] =
  "/arena/explanation/deploy/omnia/overview/";

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

function parseArgs() {
  const args = process.argv.slice(2);
  let ref = "main";
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--ref" && args[i + 1]) {
      ref = args[i + 1];
      i++;
    }
  }
  return { ref };
}

/**
 * Run `gh api` and return parsed JSON, or null on failure.
 */
function ghAPI(endpoint) {
  try {
    const raw = execSync(`gh api '${endpoint}'`, {
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
      timeout: 30_000,
    });
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

/**
 * List markdown files under SOURCE_PREFIX in the given ref.
 */
function listFiles(repo, ref) {
  const data = ghAPI(`repos/${repo}/git/trees/${ref}?recursive=1`);
  if (!data?.tree) return [];
  return data.tree
    .filter(
      (entry) =>
        entry.type === "blob" &&
        entry.path.startsWith(SOURCE_PREFIX) &&
        entry.path.endsWith(".md"),
    )
    .map((entry) => entry.path);
}

/**
 * Fetch a single file's content (base64-decoded).
 */
function fetchFile(repo, filePath, ref) {
  const data = ghAPI(`repos/${repo}/contents/${filePath}?ref=${ref}`);
  if (!data?.content) return null;
  return Buffer.from(data.content, "base64").toString("utf-8");
}

/**
 * Rewrite the sidebar.order in frontmatter to the mapped value.
 */
function rewriteFrontmatter(content, order) {
  // Replace existing sidebar.order
  const hasOrder = /^(\s*)order:\s*\d+/m;
  if (hasOrder.test(content)) {
    return content.replace(hasOrder, `$1order: ${order}`);
  }

  // If there's a sidebar: section but no order, add it
  const hasSidebar = /^sidebar:\s*$/m;
  if (hasSidebar.test(content)) {
    return content.replace(hasSidebar, `sidebar:\n  order: ${order}`);
  }

  // If no sidebar section at all, add one before the closing ---
  const parts = content.split("---");
  if (parts.length >= 3) {
    parts[1] = parts[1].trimEnd() + `\nsidebar:\n  order: ${order}\n`;
    return parts.join("---");
  }

  return content;
}

/**
 * Rewrite links in markdown content for an adapter's docs.
 */
function rewriteLinks(content, adapter) {
  return content.replace(
    /\[([^\]]*)\]\(([^)]+)\)/g,
    (match, text, url) => {
      // Skip external URLs and anchors
      if (url.startsWith("http") || url.startsWith("#") || url.startsWith("//")) {
        return match;
      }

      // Handle relative links — rewrite slug part
      if (!url.startsWith("/")) {
        for (const [oldSlug, newSlug] of Object.entries(adapter.slugMap)) {
          const patterns = [
            `../${oldSlug}/`,
            `../${oldSlug}`,
            `./${oldSlug}/`,
            `./${oldSlug}`,
            `${oldSlug}/`,
            oldSlug,
          ];
          for (const pat of patterns) {
            if (url === pat) {
              const prefix = url.startsWith("../")
                ? "../"
                : url.startsWith("./")
                  ? "./"
                  : "";
              const suffix = url.endsWith("/") ? "/" : "";
              return `[${text}](${prefix}${newSlug}${suffix})`;
            }
          }
        }
        return match;
      }

      // Handle absolute /deploy/ links → new arena paths
      for (const [oldPath, newPath] of Object.entries(DEPLOY_LINK_MAP)) {
        if (url === oldPath) {
          return `[${text}](${newPath})`;
        }
      }

      // Skip links to parent PromptKit doc sections
      if (PARENT_PREFIXES.some((prefix) => url.startsWith(prefix))) {
        return match;
      }

      // Handle absolute internal adapter links
      for (const [oldPath, newPath] of Object.entries(adapter.absoluteInternalMap)) {
        if (url === oldPath) {
          return `[${text}](${newPath})`;
        }
      }

      // Check if this looks like an internal adapter section link
      const stripped = url.replace(/^\//, "");
      const firstSegment = stripped.split("/")[0];
      if (ADAPTER_SECTIONS.includes(firstSegment)) {
        const arenaUrl = `/arena${url}`;
        return `[${text}](${arenaUrl})`;
      }

      // Unknown absolute link — leave as-is
      return match;
    },
  );
}

// ---------------------------------------------------------------------------
// Per-adapter fetch
// ---------------------------------------------------------------------------

async function fetchAdapter(adapter, ref) {
  console.log(`[fetch-adapter-docs] Fetching ${adapter.name} from ${adapter.repo}@${ref} ...`);

  const files = listFiles(adapter.repo, ref);
  if (files.length === 0) {
    console.warn(
      `[fetch-adapter-docs] Warning: no files found for ${adapter.name} (gh CLI missing or API error). Skipping.`,
    );
    return { written: 0, skipped: 0 };
  }

  let written = 0;
  let skipped = 0;
  for (const filePath of files) {
    const relativePath = filePath.slice(SOURCE_PREFIX.length);

    if (SKIP_FILES.has(relativePath)) {
      skipped++;
      continue;
    }

    const mapping = adapter.fileMap[relativePath];
    if (!mapping) {
      console.warn(
        `[fetch-adapter-docs] Warning: no mapping for ${adapter.name}:${relativePath} — skipping.`,
      );
      skipped++;
      continue;
    }

    const content = fetchFile(adapter.repo, filePath, ref);
    if (content === null) {
      console.warn(
        `[fetch-adapter-docs] Warning: failed to fetch ${filePath}`,
      );
      continue;
    }

    let rewritten = rewriteLinks(content, adapter);
    rewritten = rewriteFrontmatter(rewritten, mapping.order);

    const targetPath = path.join(ARENA_DIR, mapping.target);
    await fs.mkdir(path.dirname(targetPath), { recursive: true });
    await fs.writeFile(targetPath, rewritten, "utf-8");
    written++;
  }

  return { written, skipped };
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  if (process.env.SKIP_ADAPTER_DOCS === "1") {
    console.log("[fetch-adapter-docs] SKIP_ADAPTER_DOCS=1 — skipping.");
    return;
  }

  const { ref } = parseArgs();

  let totalWritten = 0;
  let totalSkipped = 0;

  for (const adapter of ADAPTERS) {
    const { written, skipped } = await fetchAdapter(adapter, ref);
    totalWritten += written;
    totalSkipped += skipped;
  }

  console.log(
    `[fetch-adapter-docs] Done. Wrote ${totalWritten} files, skipped ${totalSkipped}. Target: arena/`,
  );
}

main().catch((err) => {
  console.error("[fetch-adapter-docs] Error:", err.message);
  // Don't fail the build — just warn
  process.exit(0);
});
