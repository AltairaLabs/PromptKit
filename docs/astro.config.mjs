// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import d2 from 'astro-d2';

// Redirects for How-To pages that moved into themed subdirectories (see
// docs/local-backlog/2026-07-05-docs-navigation-taxonomy-design.md). In-repo
// links were rewritten to the new paths; these keep external bookmarks, search
// results, and adapter-repo links working.
const howToRedirects = {
  // SDK How-To → theme subdirs
  '/sdk/how-to/initialize/': '/sdk/how-to/conversations/initialize/',
  '/sdk/how-to/send-messages/': '/sdk/how-to/conversations/send-messages/',
  '/sdk/how-to/manage-context/': '/sdk/how-to/conversations/manage-context/',
  '/sdk/how-to/manage-state/': '/sdk/how-to/conversations/manage-state/',
  '/sdk/how-to/use-runtime-config/': '/sdk/how-to/conversations/use-runtime-config/',
  '/sdk/how-to/register-tools/': '/sdk/how-to/tools/register-tools/',
  '/sdk/how-to/client-tools/': '/sdk/how-to/tools/client-tools/',
  '/sdk/how-to/http-tools/': '/sdk/how-to/tools/http-tools/',
  '/sdk/how-to/exec-tools/': '/sdk/how-to/tools/exec-tools/',
  '/sdk/how-to/configure-mcp/': '/sdk/how-to/tools/configure-mcp/',
  '/sdk/how-to/override-capability-tools/': '/sdk/how-to/tools/override-capability-tools/',
  '/sdk/how-to/custom-hooks/': '/sdk/how-to/hooks/custom-hooks/',
  '/sdk/how-to/exec-hooks/': '/sdk/how-to/hooks/exec-hooks/',
  '/sdk/how-to/sandbox-hooks/': '/sdk/how-to/hooks/sandbox-hooks/',
  '/sdk/how-to/declarative-embedding-providers/': '/sdk/how-to/providers/declarative-embedding-providers/',
  '/sdk/how-to/declarative-tts-stt-providers/': '/sdk/how-to/providers/declarative-tts-stt-providers/',
  '/sdk/how-to/analyze-documents/': '/sdk/how-to/multimodal/analyze-documents/',
  '/sdk/how-to/preprocess-images/': '/sdk/how-to/multimodal/preprocess-images/',
  '/sdk/how-to/run-evals/': '/sdk/how-to/observability/run-evals/',
  '/sdk/how-to/monitor-events/': '/sdk/how-to/observability/monitor-events/',
  '/sdk/how-to/connect-a2a-agents/': '/sdk/how-to/interop/connect-a2a-agents/',
  // Runtime How-To → theme subdirs
  '/runtime/how-to/configure-pipeline/': '/runtime/how-to/pipeline/configure-pipeline/',
  '/runtime/how-to/streaming-responses/': '/runtime/how-to/pipeline/streaming-responses/',
  '/runtime/how-to/handle-errors/': '/runtime/how-to/pipeline/handle-errors/',
  '/runtime/how-to/setup-providers/': '/runtime/how-to/providers/setup-providers/',
  '/runtime/how-to/http-tool-mapping/': '/runtime/how-to/tools/http-tool-mapping/',
  '/runtime/how-to/integrate-mcp/': '/runtime/how-to/tools/integrate-mcp/',
  '/runtime/how-to/manage-state/': '/runtime/how-to/state/manage-state/',
  '/runtime/how-to/export-traces-otlp/': '/runtime/how-to/observability/export-traces-otlp/',
  '/runtime/how-to/monitor-costs/': '/runtime/how-to/observability/monitor-costs/',
  '/runtime/how-to/prometheus-metrics/': '/runtime/how-to/observability/prometheus-metrics/',
  '/runtime/how-to/use-a2a-mock-server/': '/runtime/how-to/a2a/use-a2a-mock-server/',
  '/runtime/how-to/use-a2a-tool-bridge/': '/runtime/how-to/a2a/use-a2a-tool-bridge/',
};

// The hand-written runtime reference page `tools-mcp.md` was replaced by
// per-package generated pages (`tools.md` + `mcp.md`, see
// docs/scripts/gen-reference.sh); redirect the retired URL so external/
// bookmarked links don't 404.
//
// The hand-written `sdk/reference/{streaming,tts,audio,variables}.md` pages
// documented runtime packages (`runtime/streaming`, `runtime/tts`,
// `runtime/audio`, `runtime/variables`) and were misfiled under sdk/
// reference; they were replaced by generated pages under runtime/reference.
const referenceRedirects = {
  '/runtime/reference/tools-mcp/': '/runtime/reference/tools/',
  '/sdk/reference/streaming/': '/runtime/reference/streaming/',
  '/sdk/reference/tts/': '/runtime/reference/tts/',
  '/sdk/reference/audio/': '/runtime/reference/audio/',
  '/sdk/reference/variables/': '/runtime/reference/variables/',
};

// The Arena deploy-adapter docs used to be fetched from the adapter repos at
// build time and published here as well as from `AltairaLabs/promptarena`,
// which owns Arena's docs and is now their sole publisher (PromptKit#1773).
// These are every `/arena/` URL this site served before the fetch was dropped;
// each has a live 1:1 counterpart on promptarena.altairalabs.ai. Astro renders
// each as a meta-refresh page carrying `robots: noindex` and a canonical link
// to the destination, so the duplicate search listings collapse onto the
// surviving copy rather than 404ing.
const ARENA_DEPLOY_PAGES = [
  'explanation/deploy/agentcore/overview',
  'explanation/deploy/agentcore/resource-lifecycle',
  'explanation/deploy/agentcore/security',
  'explanation/deploy/omnia/agent-anatomy',
  'explanation/deploy/omnia/configuration-mapping',
  'explanation/deploy/omnia/overview',
  'explanation/deploy/omnia/resource-lifecycle',
  'explanation/deploy/omnia/security',
  'how-to/deploy/agentcore/configure',
  'how-to/deploy/agentcore/dry-run',
  'how-to/deploy/agentcore/observability',
  'how-to/deploy/agentcore/tagging',
  'how-to/deploy/omnia/configure',
  'how-to/deploy/omnia/dry-run',
  'how-to/deploy/omnia/labels',
  'how-to/deploy/omnia/login',
  'reference/deploy/agentcore/configuration',
  'reference/deploy/agentcore/env-vars',
  'reference/deploy/agentcore/resource-types',
  'reference/deploy/agentcore/runtime-protocols',
  'reference/deploy/omnia/configuration',
  'reference/deploy/omnia/resource-types',
  'tutorials/deploy/agentcore/first-deployment',
  'tutorials/deploy/agentcore/multi-agent',
  'tutorials/deploy/omnia/first-deployment',
  'tutorials/deploy/omnia/multi-agent',
];

const arenaDeployRedirects = Object.fromEntries(
  ARENA_DEPLOY_PAGES.map((page) => [
    `/arena/${page}/`,
    `https://promptarena.altairalabs.ai/arena/${page}/`,
  ]),
);

// https://astro.build/config
export default defineConfig({
  site: 'https://promptkit.altairalabs.ai',
  redirects: { ...howToRedirects, ...referenceRedirects, ...arenaDeployRedirects },
  integrations: [
    d2(),
    starlight({
      title: 'PromptKit',
      logo: {
        src: './public/atlas/logo-promptkit.svg',
        alt: 'PromptKit',
      },
      customCss: ['./src/styles/custom.css'],
      // Atlas-themed code blocks: a distinct ink-void surface, mono code font,
      // hairline frame with a soft shadow, and a violet active-tab indicator —
      // so docs code blocks read as first-class, not the plain default.
      expressiveCode: {
        // Vibrant, legible syntax themes (dark = tokyo-night's blue/violet/cyan
        // fits the night-sky brand; light = github-light). Starlight swaps them
        // with data-theme.
        themes: ['tokyo-night', 'github-light'],
        styleOverrides: {
          borderColor: 'var(--hairline)',
          borderRadius: 'var(--radius-code)',
          codeBackground: 'var(--ink-void)',
          codeFontFamily: 'var(--font-mono)',
          codeFontSize: '13.5px',
          uiFontFamily: 'var(--font-sans)',
          frames: {
            editorActiveTabBackground: 'var(--ink-void)',
            editorTabBarBackground: 'var(--ink-surface)',
            editorActiveTabIndicatorBottomColor: 'var(--nebula-violet)',
            editorTabBarBorderBottomColor: 'var(--hairline)',
            terminalBackground: 'var(--ink-void)',
            terminalTitlebarBackground: 'var(--ink-surface)',
            terminalTitlebarBorderBottomColor: 'var(--hairline)',
            frameBoxShadowCssValue: '0 12px 32px -18px rgba(0, 0, 0, 0.6)',
          },
        },
      },
      // Custom Header renders the AltairaLabs masterbrand family bar as a strip
      // across the top of the fixed header, so every doc page carries the same
      // family chrome as the landing.
      components: {
        Header: './src/components/Header.astro',
      },
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/AltairaLabs/PromptKit' },
      ],
      // --- Diátaxis-first navigation ---
      // Top level is the four Diátaxis quadrants (Tutorials / How-To / Reference
      // / Explanation), with product (SDK / Runtime) as the second axis.
      // Generated sections (sdk/examples, api) wire in via autogenerate;
      // committed How-To guides are grouped by theme subdir.
      // Starlight (this version) requires `autogenerate` to live inside an
      // `items` array on a labeled group, not as a sibling of `label`.
      sidebar: [
        {
          label: 'Overview',
          items: [
            { label: 'PromptKit SDK', slug: 'sdk' },
            { label: 'Runtime', slug: 'runtime' },
          ],
        },
        {
          label: 'Tutorials',
          collapsed: true,
          items: [
            {
              label: 'SDK',
              items: [
                { autogenerate: { directory: 'sdk/tutorials' } },
                { label: 'Examples', collapsed: true, items: [{ autogenerate: { directory: 'sdk/examples' } }] },
              ],
            },
            { label: 'Runtime', items: [{ autogenerate: { directory: 'runtime/tutorials' } }] },
          ],
        },
        {
          label: 'How-To Guides',
          collapsed: true,
          items: [
            {
              label: 'SDK',
              items: [
                { label: 'Overview', slug: 'sdk/how-to' },
                { label: 'Conversations & Config', items: [{ autogenerate: { directory: 'sdk/how-to/conversations' } }] },
                { label: 'Tools & MCP', items: [{ autogenerate: { directory: 'sdk/how-to/tools' } }] },
                { label: 'Hooks', items: [{ autogenerate: { directory: 'sdk/how-to/hooks' } }] },
                { label: 'Providers', items: [{ autogenerate: { directory: 'sdk/how-to/providers' } }] },
                { label: 'Multimodal', items: [{ autogenerate: { directory: 'sdk/how-to/multimodal' } }] },
                { label: 'Evals & Observability', items: [{ autogenerate: { directory: 'sdk/how-to/observability' } }] },
                { label: 'Interop (A2A)', items: [{ autogenerate: { directory: 'sdk/how-to/interop' } }] },
              ],
            },
            {
              label: 'Runtime',
              items: [
                { label: 'Overview', slug: 'runtime/how-to' },
                { label: 'Pipeline', items: [{ autogenerate: { directory: 'runtime/how-to/pipeline' } }] },
                { label: 'Providers', items: [{ autogenerate: { directory: 'runtime/how-to/providers' } }] },
                { label: 'Tools & MCP', items: [{ autogenerate: { directory: 'runtime/how-to/tools' } }] },
                { label: 'State', items: [{ autogenerate: { directory: 'runtime/how-to/state' } }] },
                { label: 'Observability', items: [{ autogenerate: { directory: 'runtime/how-to/observability' } }] },
                { label: 'A2A', items: [{ autogenerate: { directory: 'runtime/how-to/a2a' } }] },
              ],
            },
          ],
        },
        {
          label: 'Reference',
          collapsed: true,
          items: [
            { label: 'SDK', items: [{ autogenerate: { directory: 'sdk/reference' } }] },
            { label: 'Runtime', items: [{ autogenerate: { directory: 'runtime/reference' } }] },
            { label: 'Schemas & Checks', items: [{ autogenerate: { directory: 'reference' } }] },
            { label: 'API', items: [{ autogenerate: { directory: 'api' } }] },
          ],
        },
        {
          label: 'Explanation',
          collapsed: true,
          items: [
            { label: 'Concepts', items: [{ autogenerate: { directory: 'concepts' } }] },
            { label: 'Architecture', items: [{ autogenerate: { directory: 'architecture' } }] },
            { label: 'SDK Internals', items: [{ autogenerate: { directory: 'sdk/explanation' } }] },
            { label: 'Runtime Internals', items: [{ autogenerate: { directory: 'runtime/explanation' } }] },
          ],
        },
        {
          label: 'Contributing & Project',
          collapsed: true,
          items: [
            { label: 'Contributing', items: [{ autogenerate: { directory: 'contributors' } }] },
            { label: 'CI/CD & Branch Protection', items: [{ autogenerate: { directory: 'devops' } }] },
          ],
        },
      ],
      head: [
        {
          tag: 'script',
          attrs: {
            type: 'module',
            src: '/mermaid-init.js',
          },
        },
      ],
    }),
  ],
});
