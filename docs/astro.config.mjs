// @ts-check
import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';

// https://astro.build/config
export default defineConfig({
  site: 'https://promptkit.altairalabs.ai',
  base: '/',
  integrations: [
    mdx(),
    // React removed - using native Astro components only
    sitemap(),
  ],
  markdown: {
    shikiConfig: {
      theme: 'github-dark',
    },
    remarkPlugins: [],
    rehypePlugins: [],
  },
  vite: {
    optimizeDeps: {
      exclude: ['astro:content'],
    },
  },
});
