// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import d2 from 'astro-d2';

// https://astro.build/config
export default defineConfig({
  site: 'https://promptkit.altairalabs.ai',
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
      sidebar: [
        // --- Main product sections (collapsed subsections) ---
        // Starlight v0.39+ requires `autogenerate` to live inside an `items`
        // array on a labeled group, not as a sibling of `label`/`collapsed`.
        {
          label: 'SDK',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, items: [{ autogenerate: { directory: 'sdk/tutorials' } }] },
            { label: 'How-To Guides', collapsed: true, items: [{ autogenerate: { directory: 'sdk/how-to' } }] },
            { label: 'Reference', collapsed: true, items: [{ autogenerate: { directory: 'sdk/reference' } }] },
            { label: 'Explanation', collapsed: true, items: [{ autogenerate: { directory: 'sdk/explanation' } }] },
          ],
        },
        {
          label: 'Runtime',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, items: [{ autogenerate: { directory: 'runtime/tutorials' } }] },
            { label: 'How-To Guides', collapsed: true, items: [{ autogenerate: { directory: 'runtime/how-to' } }] },
            { label: 'Reference', collapsed: true, items: [{ autogenerate: { directory: 'runtime/reference' } }] },
            { label: 'Explanation', collapsed: true, items: [{ autogenerate: { directory: 'runtime/explanation' } }] },
          ],
        },
        // --- Consolidated secondary sections ---
        {
          label: 'Concepts & Architecture',
          collapsed: true,
          items: [
            { label: 'Concepts', items: [{ autogenerate: { directory: 'concepts' } }] },
            { label: 'Architecture', items: [{ autogenerate: { directory: 'architecture' } }] },
            { label: 'Reference', items: [{ autogenerate: { directory: 'reference' } }] },
          ],
        },
        {
          label: 'API',
          collapsed: true,
          items: [{ autogenerate: { directory: 'api' } }],
        },
        {
          label: 'DevOps',
          collapsed: true,
          items: [{ autogenerate: { directory: 'devops' } }],
        },
        {
          label: 'Contributors',
          collapsed: true,
          items: [{ autogenerate: { directory: 'contributors' } }],
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
