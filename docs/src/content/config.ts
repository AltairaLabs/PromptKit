import { defineCollection, z } from 'astro:content';

// Schema for documentation pages
const docSchema = z.object({
  title: z.string(),
  description: z.string().optional(),
  product: z.enum(['arena', 'sdk', 'packc', 'runtime']).optional(),
  docType: z.enum(['tutorial', 'how-to', 'explanation', 'reference', 'guide', 'example']).optional(),
  order: z.number().optional(),
  draft: z.boolean().default(false),
  date: z.date().optional(),
  lastmod: z.date().optional(),
  tags: z.array(z.string()).optional(),
});

// Define collections for each product area
export const collections = {
  'arena': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'sdk': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'packc': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'runtime': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'concepts': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'workflows': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'examples': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'api': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'architecture': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
  'devops': defineCollection({
    type: 'content',
    schema: docSchema,
  }),
};
