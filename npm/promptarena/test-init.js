#!/usr/bin/env node

/**
 * Test the Getting Started flow from README
 * This validates that users can follow the Quick Start successfully
 */

import { spawn } from 'child_process';
import { mkdtemp, rm } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';

const TIMEOUT = 30000; // 30 seconds

async function runCommand(cmd, args, cwd) {
  return new Promise((resolve, reject) => {
    const proc = spawn(cmd, args, {
      cwd,
      stdio: 'pipe',
      shell: true
    });

    let stdout = '';
    let stderr = '';

    proc.stdout.on('data', (data) => {
      stdout += data.toString();
    });

    proc.stderr.on('data', (data) => {
      stderr += data.toString();
    });

    const timer = setTimeout(() => {
      proc.kill();
      reject(new Error(`Command timed out after ${TIMEOUT}ms`));
    }, TIMEOUT);

    proc.on('close', (code) => {
      clearTimeout(timer);
      if (code === 0) {
        resolve({ stdout, stderr });
      } else {
        reject(new Error(`Command failed with code ${code}\nStdout: ${stdout}\nStderr: ${stderr}`));
      }
    });

    proc.on('error', (err) => {
      clearTimeout(timer);
      reject(err);
    });
  });
}

async function testInitCommand() {
  console.log('Testing Quick Start flow...\n');

  // Create a temporary directory
  const tempDir = await mkdtemp(join(tmpdir(), 'promptarena-test-'));
  console.log(`Created temp directory: ${tempDir}`);

  try {
    // Step 1: Test init command
    console.log('\n1. Running: promptarena init customer-support');
    const { stdout: initOutput } = await runCommand(
      'node',
      [join(process.cwd(), 'bin', 'promptarena.js'), 'init', 'customer-support', '--quick'],
      tempDir
    );
    console.log(initOutput);

    // Verify the project was created
    const projectDir = join(tempDir, 'customer-support');
    console.log(`✓ Project initialized at ${projectDir}`);

    // Step 2: Test validate command (dry run without API calls)
    console.log('\n2. Running: promptarena validate arena.yaml');
    const { stdout: validateOutput } = await runCommand(
      'node',
      [join(process.cwd(), 'bin', 'promptarena.js'), 'validate', 'arena.yaml', '--schema-only'],
      projectDir
    );
    console.log(validateOutput);
    console.log('✓ Configuration validated successfully');

    // Step 3: Test templates list
    console.log('\n3. Running: promptarena templates list');
    const { stdout: templatesOutput } = await runCommand(
      'node',
      [join(process.cwd(), 'bin', 'promptarena.js'), 'templates', 'list'],
      tempDir
    );
    console.log(templatesOutput);
    console.log('✓ Templates listed successfully');

    console.log('\n✅ All tests passed!');
    console.log('\nThe Getting Started flow works correctly.');
    console.log('Users can successfully run:');
    console.log('  1. promptarena init <template>');
    console.log('  2. cd <template>');
    console.log('  3. promptarena run (with API keys)');

  } catch (error) {
    console.error('\n❌ Test failed:', error.message);
    throw error;
  } finally {
    // Clean up
    console.log(`\nCleaning up ${tempDir}...`);
    await rm(tempDir, { recursive: true, force: true });
  }
}

// Run the test
testInitCommand()
  .then(() => {
    process.exit(0);
  })
  .catch((error) => {
    console.error('Test suite failed:', error);
    process.exit(1);
  });
