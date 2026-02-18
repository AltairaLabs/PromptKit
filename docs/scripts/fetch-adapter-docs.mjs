#!/usr/bin/env node

/**
 * Fetches adapter documentation from external repos at build time and
 * maps them into the arena/ documentation sections.
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

const REPO = "AltairaLabs/promptarena-deploy-agentcore";
const SOURCE_PREFIX = "docs/src/content/docs/";

// Mapping from upstream file paths (relative to SOURCE_PREFIX) to arena deploy targets.
const FILE_MAP = {
  "index.md": { target: "explanation/deploy/agentcore/overview.md", order: 50 },
  "tutorials/01-first-deployment.md": {
    target: "tutorials/deploy/agentcore/first-deployment.md",
    order: 50,
  },
  "tutorials/02-multi-agent.md": {
    target: "tutorials/deploy/agentcore/multi-agent.md",
    order: 50,
  },
  "how-to/configure.md": {
    target: "how-to/deploy/agentcore/configure.md",
    order: 50,
  },
  "how-to/dry-run.md": {
    target: "how-to/deploy/agentcore/dry-run.md",
    order: 50,
  },
  "how-to/tagging.md": {
    target: "how-to/deploy/agentcore/tagging.md",
    order: 50,
  },
  "how-to/observability.md": {
    target: "how-to/deploy/agentcore/observability.md",
    order: 50,
  },
  "reference/configuration.md": {
    target: "reference/deploy/agentcore/configuration.md",
    order: 50,
  },
  "reference/resource-types.md": {
    target: "reference/deploy/agentcore/resource-types.md",
    order: 50,
  },
  "reference/environment-variables.md": {
    target: "reference/deploy/agentcore/env-vars.md",
    order: 50,
  },
  "explanation/resource-lifecycle.md": {
    target: "explanation/deploy/agentcore/resource-lifecycle.md",
    order: 50,
  },
  "explanation/security.md": {
    target: "explanation/deploy/agentcore/security.md",
    order: 50,
  },
};

// Skip upstream index pages — arena already has section indexes.
const SKIP_FILES = new Set([
  "tutorials/index.md",
  "how-to/index.md",
  "reference/index.md",
  "explanation/index.md",
]);

// Mapping of upstream internal relative slugs to their new agentcore filenames.
// Used to rewrite same-section relative links.
const SLUG_MAP = {
  "01-first-deployment": "first-deployment",
  "02-multi-agent": "multi-agent",
  configure: "configure",
  "dry-run": "dry-run",
  tagging: "tagging",
  observability: "observability",
  configuration: "configuration",
  "resource-types": "resource-types",
  "environment-variables": "env-vars",
  "resource-lifecycle": "resource-lifecycle",
  security: "security",
};

// Mapping from upstream absolute internal paths to new arena paths.
// These handle links like /reference/configuration/ → /arena/reference/deploy/agentcore/configuration/
const ABSOLUTE_INTERNAL_MAP = {
  "/tutorials/01-first-deployment/":
    "/arena/tutorials/deploy/agentcore/first-deployment/",
  "/tutorials/02-multi-agent/":
    "/arena/tutorials/deploy/agentcore/multi-agent/",
  "/how-to/configure/": "/arena/how-to/deploy/agentcore/configure/",
  "/how-to/dry-run/": "/arena/how-to/deploy/agentcore/dry-run/",
  "/how-to/tagging/": "/arena/how-to/deploy/agentcore/tagging/",
  "/how-to/observability/": "/arena/how-to/deploy/agentcore/observability/",
  "/reference/configuration/":
    "/arena/reference/deploy/agentcore/configuration/",
  "/reference/resource-types/":
    "/arena/reference/deploy/agentcore/resource-types/",
  "/reference/environment-variables/":
    "/arena/reference/deploy/agentcore/env-vars/",
  "/explanation/resource-lifecycle/":
    "/arena/explanation/deploy/agentcore/resource-lifecycle/",
  "/explanation/security/": "/arena/explanation/deploy/agentcore/security/",
};

// Mapping from old /deploy/ absolute links to their new arena paths.
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
  "/deploy/adapters/agentcore/":
    "/arena/explanation/deploy/agentcore/overview/",
};

// Absolute link prefixes that belong to parent PromptKit docs — leave as-is.
const PARENT_PREFIXES = ["/packc/", "/sdk/", "/arena/", "/runtime/"];

// Sections that exist inside the adapter docs (used to detect internal absolute links).
const ADAPTER_SECTIONS = [
  "how-to",
  "reference",
  "explanation",
  "tutorials",
];

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
function listFiles(ref) {
  const data = ghAPI(`repos/${REPO}/git/trees/${ref}?recursive=1`);
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
function fetchFile(filePath, ref) {
  const data = ghAPI(`repos/${REPO}/contents/${filePath}?ref=${ref}`);
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
  // Find the second --- (end of frontmatter)
  const parts = content.split("---");
  if (parts.length >= 3) {
    parts[1] = parts[1].trimEnd() + `\nsidebar:\n  order: ${order}\n`;
    return parts.join("---");
  }

  return content;
}

/**
 * Rewrite links in markdown content for agentcore adapter docs.
 *
 * Handles:
 * - Relative same-section links (e.g., ../dry-run/) → ../agentcore-dry-run/
 * - Absolute internal links (/reference/configuration/) → /arena/reference/agentcore-configuration/
 * - Absolute /deploy/ links → new arena paths
 * - External URLs and parent doc links → unchanged
 */
function rewriteLinks(content) {
  return content.replace(
    /\[([^\]]*)\]\(([^)]+)\)/g,
    (match, text, url) => {
      // Skip external URLs and anchors
      if (url.startsWith("http") || url.startsWith("#") || url.startsWith("//")) {
        return match;
      }

      // Handle relative links — rewrite slug part
      if (!url.startsWith("/")) {
        // Extract the slug from relative paths like ../dry-run/ or ./configure/
        for (const [oldSlug, newSlug] of Object.entries(SLUG_MAP)) {
          // Match patterns like ../dry-run/, ../dry-run, ./dry-run/, dry-run/
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
              // Preserve the relative prefix style
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

      // Handle absolute internal adapter links (/reference/configuration/)
      for (const [oldPath, newPath] of Object.entries(ABSOLUTE_INTERNAL_MAP)) {
        if (url === oldPath) {
          return `[${text}](${newPath})`;
        }
      }

      // Check if this looks like an internal adapter section link
      const stripped = url.replace(/^\//, "");
      const firstSegment = stripped.split("/")[0];
      if (ADAPTER_SECTIONS.includes(firstSegment)) {
        // Fallback: prefix with /arena/ and try to match
        const arenaUrl = `/arena${url}`;
        return `[${text}](${arenaUrl})`;
      }

      // Unknown absolute link — leave as-is
      return match;
    },
  );
}

async function main() {
  if (process.env.SKIP_ADAPTER_DOCS === "1") {
    console.log("[fetch-adapter-docs] SKIP_ADAPTER_DOCS=1 — skipping.");
    return;
  }

  const { ref } = parseArgs();
  console.log(`[fetch-adapter-docs] Fetching from ${REPO}@${ref} ...`);

  const files = listFiles(ref);
  if (files.length === 0) {
    console.warn(
      "[fetch-adapter-docs] Warning: no files found (gh CLI missing or API error). Skipping.",
    );
    return;
  }

  let written = 0;
  let skipped = 0;
  for (const filePath of files) {
    const relativePath = filePath.slice(SOURCE_PREFIX.length);

    // Skip upstream index pages
    if (SKIP_FILES.has(relativePath)) {
      skipped++;
      continue;
    }

    // Look up the mapping
    const mapping = FILE_MAP[relativePath];
    if (!mapping) {
      console.warn(
        `[fetch-adapter-docs] Warning: no mapping for ${relativePath} — skipping.`,
      );
      skipped++;
      continue;
    }

    const content = fetchFile(filePath, ref);
    if (content === null) {
      console.warn(
        `[fetch-adapter-docs] Warning: failed to fetch ${filePath}`,
      );
      continue;
    }

    let rewritten = rewriteLinks(content);
    rewritten = rewriteFrontmatter(rewritten, mapping.order);

    const targetPath = path.join(ARENA_DIR, mapping.target);
    await fs.mkdir(path.dirname(targetPath), { recursive: true });
    await fs.writeFile(targetPath, rewritten, "utf-8");
    written++;
  }

  console.log(
    `[fetch-adapter-docs] Wrote ${written} files, skipped ${skipped}. Target: arena/`,
  );
}

main().catch((err) => {
  console.error("[fetch-adapter-docs] Error:", err.message);
  // Don't fail the build — just warn
  process.exit(0);
});
