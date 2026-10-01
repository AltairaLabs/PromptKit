import { defineCollection } from 'astro:content';
import { z } from 'astro:schema';
import { docsSchema } from '@astrojs/starlight/schema';
import { docsLoader } from '@astrojs/starlight/loaders';

export const collections = {
  docs: defineCollection({
    loader: docsLoader(),
    schema: docsSchema({
      extend: z.object({
        // Code-verification record written by /docs-review and read by
        // scripts/docs-stale.sh. See docs/STYLE.md "Keeping pages true".
        verified: z
          .object({
            commit: z.string(),
            sources: z.array(z.string()),
          })
          .optional(),
      }),
    }),
  }),
};
