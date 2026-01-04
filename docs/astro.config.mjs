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
      plugins: [starlightThemeGalaxy()],
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/AltairaLabs/PromptKit' },
      ],
      sidebar: [
        { label: 'Arena', autogenerate: { directory: 'arena' } },
        { label: 'SDK', autogenerate: { directory: 'sdk' } },
        { label: 'PackC', autogenerate: { directory: 'packc' } },
        { label: 'Runtime', autogenerate: { directory: 'runtime' } },
        { label: 'Concepts', autogenerate: { directory: 'concepts' } },
        { label: 'Workflows', autogenerate: { directory: 'workflows' } },
        { label: 'Architecture', autogenerate: { directory: 'architecture' } },
        { label: 'API', autogenerate: { directory: 'api' } },
        { label: 'DevOps', autogenerate: { directory: 'devops' } },
        { label: 'Contributors', autogenerate: { directory: 'contributors' } },
        { label: 'Reference', autogenerate: { directory: 'reference' } },
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
