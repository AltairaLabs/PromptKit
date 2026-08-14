# PromptKit Documentation (Astro)

This directory contains the new Astro-based documentation for PromptKit, replacing the previous Jekyll implementation.

## Quick Start

### Install Dependencies

```bash
make docs-install
```

Or directly:

```bash
npm install
```

### Local Development

```bash
make docs-serve
```

Or from this directory:

```bash
npm run dev
```

Visit http://localhost:4321/promptkit

### Build for Production

```bash
make docs-build
```

The built site will be in `dist/`

## Project Structure

```
docs-new/
├── src/
│   ├── content/          # Content collections (markdown files)
│   │   ├── sdk/          # SDK documentation
│   │   ├── runtime/      # Runtime documentation
│   │   ├── concepts/     # Cross-cutting concepts
│   │   ├── examples/     # Generated examples
│   │   └── api/          # Generated API docs
│   ├── components/       # Reusable components
│   ├── layouts/          # Page layouts
│   └── pages/            # Route pages
├── scripts/
│   └── migrate.mjs       # Migration script
└── astro.config.mjs
```

## Key Features

- ⚡ **10-100x faster builds** than Jekyll
- 🎨 **Modern component system** (React/Vue/Svelte)
- 🔧 **Type-safe content** with Zod schemas  
- 🚀 **Hot module reloading** for instant feedback
- 📦 **Simpler dependencies** (npm vs Ruby gems)

## Commands

From the repository root:

| Command            | Action                                    |
| :----------------- | :---------------------------------------- |
| `make docs-install`| Install documentation dependencies        |
| `make docs-serve`  | Start local dev server                    |
| `make docs-build`  | Build production site                     |
| `make docs-preview`| Preview production build                  |
| `make docs-clean`  | Clean generated files                     |

From this directory:

| Command          | Action                                      |
| :--------------- | :------------------------------------------ |
| `npm install`    | Install dependencies                        |
| `npm run dev`    | Start dev server at `localhost:4321`        |
| `npm run build`  | Build production site to `./dist/`          |
| `npm run preview`| Preview production build locally            |

## Adding Content

1. Create a markdown file in the appropriate `src/content/` collection
2. Add frontmatter:
   ```yaml
   ---
   title: Your Page Title
   description: Optional description
   docType: tutorial|how-to|explanation|reference
   ---
   ```
3. Write your content
4. Navigation updates automatically!

## Migration from Jekyll

This documentation was migrated from Jekyll. Key improvements:

- **No Liquid templates** - Use Astro components
- **Automatic navigation** - No manual ordering
- **Faster builds** - Seconds instead of minutes
- **Modern tooling** - TypeScript, npm, hot reload

See the [migration proposal](../docs/local-backlog/astro-migration-proposal.md) for details.

## Resources

- [Astro Documentation](https://docs.astro.build/)
- [Content Collections Guide](https://docs.astro.build/en/guides/content-collections/)
- [PromptKit Main README](../README.md)
