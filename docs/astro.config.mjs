// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeGalaxy from 'starlight-theme-galaxy';
import d2 from 'astro-d2';

// https://astro.build/config
export default defineConfig({
  site: 'https://promptkit.altairalabs.ai',
  integrations: [
    d2(),
    starlight({
      title: 'PromptKit',
      logo: {
        src: './public/logo.svg',
        alt: 'PromptKit Logo',
      },
      plugins: [starlightThemeGalaxy()],
      customCss: ['./src/styles/custom.css'],
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/AltairaLabs/PromptKit' },
      ],
      sidebar: [
        // --- Main product sections (collapsed subsections) ---
        // Starlight v0.39+ requires `autogenerate` to live inside an `items`
        // array on a labeled group, not as a sibling of `label`/`collapsed`.
        {
          label: 'Arena',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, items: [{ autogenerate: { directory: 'arena/tutorials' } }] },
            { label: 'How-To Guides', collapsed: true, items: [{ autogenerate: { directory: 'arena/how-to' } }] },
            { label: 'Reference', collapsed: true, items: [{ autogenerate: { directory: 'arena/reference' } }] },
            { label: 'Explanation', collapsed: true, items: [{ autogenerate: { directory: 'arena/explanation' } }] },
          ],
        },
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
          label: 'PackC',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, items: [{ autogenerate: { directory: 'packc/tutorials' } }] },
            { label: 'How-To Guides', collapsed: true, items: [{ autogenerate: { directory: 'packc/how-to' } }] },
            { label: 'Reference', collapsed: true, items: [{ autogenerate: { directory: 'packc/reference' } }] },
            { label: 'Explanation', collapsed: true, items: [{ autogenerate: { directory: 'packc/explanation' } }] },
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
