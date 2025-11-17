#!/usr/bin/env node
import { LinkChecker } from 'linkinator';
import { spawn } from 'child_process';

// Start preview server
console.log('🚀 Starting preview server...');
const server = spawn('npm', ['run', 'preview'], { 
  detached: true,
  stdio: 'ignore'
});

// Wait for server to be ready
await new Promise(resolve => setTimeout(resolve, 4000));

try {
  console.log('🔍 Checking all links...\n');
  
  const checker = new LinkChecker();
  const result = await checker.check({
    path: 'http://localhost:4321/promptkit',
    recurse: true,
    timeout: 10000,
  });

  console.log(`\n📊 Total links checked: ${result.links.length}\n`);

  // Just filter broken links - keep it simple
  const brokenLinks = result.links.filter(link => link.state === 'BROKEN');
  
  if (brokenLinks.length === 0) {
    console.log('✅ No broken links found!');
    process.exit(0);
  }
  
  // Group by broken URL
  const linksByUrl = new Map();
  for (const link of brokenLinks) {
    if (!linksByUrl.has(link.url)) {
      linksByUrl.set(link.url, []);
    }
    linksByUrl.get(link.url).push(link.parent);
  }
  
  // Separate internal and external
  const internal = [];
  const external = [];
  
  for (const [url, parents] of linksByUrl) {
    const entry = { url, parents: [...new Set(parents)] };
    if (url.startsWith('http://localhost:4321') || url.startsWith('/')) {
      internal.push(entry);
    } else {
      external.push(entry);
    }
  }
  
  console.log(`❌ Found ${brokenLinks.length} broken links (${linksByUrl.size} unique URLs)\n`);
  
  if (internal.length > 0) {
    console.log(`🔴 INTERNAL BROKEN LINKS (${internal.length}):\n`);
    
    const displayCount = Math.min(50, internal.length);
    for (let i = 0; i < displayCount; i++) {
      const { url, parents } = internal[i];
      console.log(`  [404] ${url}`);
      console.log(`      Found on ${parents.length} page(s):`);
      parents.slice(0, 3).forEach(p => console.log(`        - ${p}`));
      if (parents.length > 3) {
        console.log(`        ... and ${parents.length - 3} more`);
      }
      console.log('');
    }
    
    if (internal.length > 50) {
      console.log(`... and ${internal.length - 50} more internal broken links\n`);
    }
  }
  
  if (external.length > 0) {
    console.log(`\n🌐 EXTERNAL BROKEN LINKS (${external.length}):\n`);
    
    for (const { url, parents } of external) {
      console.log(`  [404] ${url}`);
      console.log(`      Found on ${parents.length} page(s):`);
      parents.slice(0, 3).forEach(p => console.log(`        - ${p}`));
      if (parents.length > 3) {
        console.log(`        ... and ${parents.length - 3} more`);
      }
      console.log('');
    }
  }
  
  process.exit(1);
} catch (error) {
  console.error('Error:', error.message);
  process.exit(1);
} finally {
  try {
    process.kill(-server.pid);
  } catch (e) {
    // Ignore
  }
}
