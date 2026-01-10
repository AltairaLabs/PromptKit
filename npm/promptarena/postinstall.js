#!/usr/bin/env node

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { install, getDownloadUrl, getPlatformInfo } from './lib/installer.js';

const require = createRequire(import.meta.url);
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const BINARY_NAME = 'promptarena';
const VERSION = require('./package.json').version;

try {
  await install(BINARY_NAME, VERSION, __dirname);
} catch (error) {
  const { platform, arch } = getPlatformInfo();
  const url = getDownloadUrl(VERSION, platform, arch);

  console.error(`\n❌ Installation failed: ${error.message}`);
  console.error('\nTroubleshooting:');
  console.error('1. Verify version exists: https://github.com/AltairaLabs/PromptKit/releases');
  console.error('2. Check your internet connection');
  console.error('3. Try downloading manually from GitHub Releases');
  console.error(`   URL: ${url}`);
  process.exit(1);
}
