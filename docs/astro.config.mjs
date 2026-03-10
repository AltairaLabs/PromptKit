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
        {
          label: 'Arena',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, autogenerate: { directory: 'arena/tutorials' } },
            { label: 'How-To Guides', collapsed: true, autogenerate: { directory: 'arena/how-to' } },
            { label: 'Reference', collapsed: true, autogenerate: { directory: 'arena/reference' } },
            { label: 'Explanation', collapsed: true, autogenerate: { directory: 'arena/explanation' } },
          ],
        },
        {
          label: 'SDK',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, autogenerate: { directory: 'sdk/tutorials' } },
            { label: 'How-To Guides', collapsed: true, autogenerate: { directory: 'sdk/how-to' } },
            { label: 'Reference', collapsed: true, autogenerate: { directory: 'sdk/reference' } },
            { label: 'Explanation', collapsed: true, autogenerate: { directory: 'sdk/explanation' } },
          ],
        },
        {
          label: 'PackC',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, autogenerate: { directory: 'packc/tutorials' } },
            { label: 'How-To Guides', collapsed: true, autogenerate: { directory: 'packc/how-to' } },
            { label: 'Reference', collapsed: true, autogenerate: { directory: 'packc/reference' } },
            { label: 'Explanation', collapsed: true, autogenerate: { directory: 'packc/explanation' } },
          ],
        },
        {
          label: 'Runtime',
          collapsed: true,
          items: [
            { label: 'Tutorials', collapsed: true, autogenerate: { directory: 'runtime/tutorials' } },
            { label: 'How-To Guides', collapsed: true, autogenerate: { directory: 'runtime/how-to' } },
            { label: 'Reference', collapsed: true, autogenerate: { directory: 'runtime/reference' } },
            { label: 'Explanation', collapsed: true, autogenerate: { directory: 'runtime/explanation' } },
          ],
        },
        // --- Consolidated secondary sections ---
        {
          label: 'Concepts & Architecture',
          collapsed: true,
          items: [
            { label: 'Concepts', autogenerate: { directory: 'concepts' } },
            { label: 'Architecture', autogenerate: { directory: 'architecture' } },
            { label: 'Reference', autogenerate: { directory: 'reference' } },
          ],
        },
        {
          label: 'API',
          collapsed: true,
          autogenerate: { directory: 'api' },
        },
        {
          label: 'DevOps',
          collapsed: true,
          autogenerate: { directory: 'devops' },
        },
        {
          label: 'Contributors',
          collapsed: true,
          autogenerate: { directory: 'contributors' },
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
