#!/usr/bin/env node

const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

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
