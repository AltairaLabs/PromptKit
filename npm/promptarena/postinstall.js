#!/usr/bin/env node

import https from 'node:https';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { pipeline } from 'node:stream';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const streamPipeline = promisify(pipeline);

const BINARY_NAME = 'promptarena';
const GITHUB_REPO = 'AltairaLabs/PromptKit';
const VERSION = require('./package.json').version;

// Platform mapping to match GoReleaser output
const PLATFORM_MAP = {
  darwin: 'Darwin',
  linux: 'Linux',
  win32: 'Windows'
};

const ARCH_MAP = {
  x64: 'x86_64',
  arm64: 'arm64'
};

function getPlatformInfo() {
  const platform = PLATFORM_MAP[process.platform];
  const arch = ARCH_MAP[process.arch];
  
  if (!platform || !arch) {
    throw new Error(`Unsupported platform: ${process.platform}-${process.arch}`);
  }
  
  return { platform, arch };
}

function getDownloadUrl(platform, arch) {
  const archiveExt = platform === 'Windows' ? 'zip' : 'tar.gz';
  const archiveName = `PromptKit_${VERSION}_${platform}_${arch}.${archiveExt}`;
  return `https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/${archiveName}`;
}

async function downloadFile(url, destPath) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith('https:') ? https : http;
    
    client.get(url, (response) => {
      // Follow redirects
      if (response.statusCode === 302 || response.statusCode === 301) {
        downloadFile(response.headers.location, destPath)
          .then(resolve)
          .catch(reject);
        return;
      }
      
      if (response.statusCode !== 200) {
        reject(new Error(`Failed to download: HTTP ${response.statusCode}`));
        return;
      }
      
      const fileStream = fs.createWriteStream(destPath);
      streamPipeline(response, fileStream)
        .then(resolve)
        .catch(reject);
    }).on('error', reject);
  });
}

function extractBinary(archivePath, platform, binaryName) {
  const binaryWithExt = platform === 'Windows' ? `${binaryName}.exe` : binaryName;
  const destPath = path.join(__dirname, binaryWithExt);
  
  try {
    if (platform === 'Windows') {
      // Extract from zip - the binary should be in the archive root
      execSync(`unzip -j "${archivePath}" "${binaryWithExt}" -d "${__dirname}"`, {
        stdio: 'inherit'
      });
    } else {
      // Extract from tar.gz - the binary should be in the archive root
      execSync(`tar -xzf "${archivePath}" -C "${__dirname}" "${binaryWithExt}"`, {
        stdio: 'inherit'
      });
    }
    
    // Make executable on Unix-like systems
    if (platform !== 'Windows') {
      fs.chmodSync(destPath, 0o755);
    }
    
    console.log(`✓ Extracted ${binaryWithExt}`);
    return destPath;
  } catch (error) {
    throw new Error(`Failed to extract binary: ${error.message}`);
  }
}

async function install() {
  console.log(`Installing ${BINARY_NAME} v${VERSION}...`);
  
  const { platform, arch } = getPlatformInfo();
  console.log(`Platform: ${platform} ${arch}`);
  
  const url = getDownloadUrl(platform, arch);
  const archiveExt = platform === 'Windows' ? 'zip' : 'tar.gz';
  const archivePath = path.join(__dirname, `archive.${archiveExt}`);
  
  try {
    console.log('Downloading binary from GitHub Releases...');
    await downloadFile(url, archivePath);
    console.log('✓ Download complete');
    
    console.log('Extracting binary...');
    extractBinary(archivePath, platform, BINARY_NAME);
    
    // Clean up archive
    fs.unlinkSync(archivePath);
    
    console.log(`✓ ${BINARY_NAME} installed successfully!`);
  } catch (error) {
    console.error(`\n❌ Installation failed: ${error.message}`);
    console.error('\nTroubleshooting:');
    console.error('1. Verify version exists: https://github.com/AltairaLabs/PromptKit/releases');
    console.error('2. Check your internet connection');
    console.error('3. Try downloading manually from GitHub Releases');
    console.error(`   URL: ${url}`);
    process.exit(1);
  }
}

await install();
