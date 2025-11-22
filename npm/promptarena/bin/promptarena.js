#!/usr/bin/env node

import { spawn } from 'node:child_process';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const binaryName = process.platform === 'win32' ? 'promptarena.exe' : 'promptarena';
const binaryPath = path.join(__dirname, '..', binaryName);

// Check if binary exists
if (!fs.existsSync(binaryPath)) {
  console.error('Error: promptarena binary not found.');
  console.error('Please try reinstalling: npm install @altairalabs/promptarena');
  process.exit(1);
}

// Spawn the Go binary with all arguments
const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: 'inherit',
  windowsHide: false
});

child.on('error', (err) => {
  console.error('Failed to start promptarena:', err.message);
  process.exit(1);
});

child.on('exit', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code || 0);
  }
});
