#!/usr/bin/env node

/**
 * Migration script to convert Jekyll documentation to Astro
 * 
 * This script:
 * 1. Reads Jekyll markdown files from docs/
 * 2. Transforms frontmatter from Jekyll format to Astro format
 * 3. Cleans up Liquid syntax
 * 4. Writes files to docs-new/src/content/
 */

import fs from 'fs/promises';
import path from 'path';
import { fileURLToPath } from 'url';
import matter from 'gray-matter';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const ROOT_DIR = path.join(__dirname, '..', '..');
const JEKYLL_DOCS_DIR = path.join(ROOT_DIR, 'docs-jekyll-backup');
const ASTRO_CONTENT_DIR = path.join(ROOT_DIR, 'docs', 'src', 'content');

// Collections to migrate
const COLLECTIONS = ['arena', 'sdk', 'packc', 'runtime', 'concepts', 'workflows', 'architecture', 'devops'];

/**
 * Transform Jekyll frontmatter to Astro format
 * @param {Object} jekyll - Jekyll frontmatter object
 * @param {string} filePath - File path for context
 * @returns {Object} Astro frontmatter object
 */
function transformFrontmatter(jekyll, filePath) {
  const astro = {
    title: jekyll.title,
  };

  // Map description
  if (jekyll.description) {
    astro.description = jekyll.description;
  }

  // Map product
  if (jekyll.product) {
    astro.product = jekyll.product;
  }

  // Map doc_type to docType
  if (jekyll.doc_type) {
    astro.docType = jekyll.doc_type;
  } else {
    // Try to detect from path
    astro.docType = detectDocType(filePath);
  }

  // Map nav_order to order
  if (jekyll.nav_order !== undefined) {
    astro.order = jekyll.nav_order;
  }

  // Check if draft (files starting with underscore or in drafts folder)
  const fileName = path.basename(filePath);
  if (fileName.startsWith('_') || filePath.includes('/_drafts/')) {
    astro.draft = true;
  }

  return astro;
}

/**
 * Detect document type from file path
 * @param {string} filePath - File path
 * @returns {string|undefined} Document type
 */
function detectDocType(filePath) {
  const pathLower = filePath.toLowerCase();
  
  if (pathLower.includes('/tutorials/')) return 'tutorial';
  if (pathLower.includes('/how-to/')) return 'how-to';
  if (pathLower.includes('/explanation/')) return 'explanation';
  if (pathLower.includes('/reference/')) return 'reference';
  
  return 'guide';
}

/**
 * Clean Jekyll-specific syntax from content
 * @param {string} content - Markdown content
 * @returns {string} Cleaned content
 */
function cleanContent(content) {
  let cleaned = content;

  // Remove Jekyll includes
  cleaned = cleaned.replace(/\{\%\s*include\s+[^\s%]+\s*\%\}/g, '');

  // Replace site.baseurl with /promptkit
  cleaned = cleaned.replace(/\{\{\s*site\.baseurl\s*\}\}/g, '/promptkit');

  // Replace page.url
  cleaned = cleaned.replace(/\{\{\s*page\.url\s*\}\}/g, '');

  // Remove other Liquid variables (keep the text, remove the liquid syntax)
  cleaned = cleaned.replace(/\{\{\s*([^}]+)\s*\}\}/g, '');

  // Remove Liquid tags
  cleaned = cleaned.replace(/\{\%[^%]*\%\}/g, '');

  // Fix Jekyll-style links (remove .html, .md extensions)
  cleaned = cleaned.replace(/(\[.*?\]\([^)]+)\.(html|md)(\))/g, '$1$3');

  // Clean up multiple blank lines
  cleaned = cleaned.replace(/\n{3,}/g, '\n\n');

  return cleaned.trim();
}

/**
 * Process a single markdown file
 * @param {string} sourcePath - Source file path
 * @param {string} targetPath - Target file path
 */
async function processFile(sourcePath, targetPath) {
  try {
    const fileContent = await fs.readFile(sourcePath, 'utf-8');
    const { data, content } = matter(fileContent);

    // Transform frontmatter
    const astroFrontmatter = transformFrontmatter(data, sourcePath);

    // Clean content
    const cleanedContent = cleanContent(content);

    // Create new content with transformed frontmatter
    const newContent = matter.stringify(cleanedContent, astroFrontmatter);

    // Ensure target directory exists
    const targetDir = path.dirname(targetPath);
    await fs.mkdir(targetDir, { recursive: true });

    // Write file
    await fs.writeFile(targetPath, newContent, 'utf-8');

    console.log(`✓ Migrated: ${path.relative(ROOT_DIR, sourcePath)} -> ${path.relative(ROOT_DIR, targetPath)}`);
  } catch (error) {
    console.error(`✗ Error processing ${sourcePath}:`, error instanceof Error ? error.message : error);
  }
}

/**
 * Recursively find all markdown files in a directory
 * @param {string} dir - Directory path
 * @returns {Promise<string[]>} Array of file paths
 */
async function findMarkdownFiles(dir) {
  const files = [];
  
  try {
    const entries = await fs.readdir(dir, { withFileTypes: true });
    
    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);
      
      if (entry.isDirectory()) {
        // Skip certain directories
        if (entry.name === '_site' || entry.name === 'node_modules' || entry.name === '.git') {
          continue;
        }
        
        // Recursively search subdirectories
        const subFiles = await findMarkdownFiles(fullPath);
        files.push(...subFiles);
      } else if (entry.isFile() && (entry.name.endsWith('.md') || entry.name.endsWith('.markdown'))) {
        // Skip README.md and index.md at root level
        if (entry.name === 'README.md' && dir === JEKYLL_DOCS_DIR) {
          continue;
        }
        
        files.push(fullPath);
      }
    }
  } catch (error) {
    // Directory might not exist
  }
  
  return files;
}

/**
 * Migrate a collection
 * @param {string} collectionName - Collection name
 */
async function migrateCollection(collectionName) {
  console.log(`\n📂 Migrating collection: ${collectionName}`);
  
  const sourceDir = path.join(JEKYLL_DOCS_DIR, collectionName);
  const targetDir = path.join(ASTRO_CONTENT_DIR, collectionName);
  
  try {
    // Check if source directory exists
    await fs.access(sourceDir);
  } catch {
    console.log(`   ⚠️  Source directory not found: ${sourceDir}`);
    return;
  }
  
  // Find all markdown files
  const markdownFiles = await findMarkdownFiles(sourceDir);
  
  if (markdownFiles.length === 0) {
    console.log(`   No markdown files found in ${collectionName}`);
    return;
  }
  
  console.log(`   Found ${markdownFiles.length} files`);
  
  // Process each file
  for (const sourceFile of markdownFiles) {
    // Calculate relative path
    const relativePath = path.relative(sourceDir, sourceFile);
    const targetFile = path.join(targetDir, relativePath);
    
    await processFile(sourceFile, targetFile);
  }
}

/**
 * Main migration function
 */
async function main() {
  console.log('🚀 Starting Jekyll to Astro migration...\n');
  console.log(`Source: ${JEKYLL_DOCS_DIR}`);
  console.log(`Target: ${ASTRO_CONTENT_DIR}\n`);
  
  // Migrate each collection
  for (const collection of COLLECTIONS) {
    await migrateCollection(collection);
  }
  
  console.log('\n✅ Migration complete!\n');
  console.log('Next steps:');
  console.log('  1. Review migrated content in docs/src/content/');
  console.log('  2. Run: cd docs && npm run dev');
  console.log('  3. Test the documentation site');
  console.log('  4. Fix any remaining issues\n');
}

// Run migration
main().catch(error => {
  console.error('❌ Migration failed:', error);
  process.exit(1);
});
