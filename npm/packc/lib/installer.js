import https from 'node:https';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { pipeline } from 'node:stream';
import { promisify } from 'node:util';

const streamPipeline = promisify(pipeline);

// Platform mapping to match GoReleaser output
export const PLATFORM_MAP = {
  darwin: 'Darwin',
  linux: 'Linux',
  win32: 'Windows'
};

export const ARCH_MAP = {
  x64: 'x86_64',
  arm64: 'arm64'
};

export function getPlatformInfo(platform = process.platform, arch = process.arch) {
  const mappedPlatform = PLATFORM_MAP[platform];
  const mappedArch = ARCH_MAP[arch];

  if (!mappedPlatform || !mappedArch) {
    throw new Error(`Unsupported platform: ${platform}-${arch}`);
  }

  return { platform: mappedPlatform, arch: mappedArch };
}

export function getDownloadUrl(version, platform, arch, repo = 'AltairaLabs/PromptKit') {
  const archiveExt = platform === 'Windows' ? 'zip' : 'tar.gz';
  const archiveName = `PromptKit_${version}_${platform}_${arch}.${archiveExt}`;
  return `https://github.com/${repo}/releases/download/v${version}/${archiveName}`;
}

export async function downloadFile(url, destPath) {
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

export function extractBinary(archivePath, platform, binaryName, destDir) {
  const binaryWithExt = platform === 'Windows' ? `${binaryName}.exe` : binaryName;
  const destPath = path.join(destDir, binaryWithExt);

  if (platform === 'Windows') {
    execSync(`unzip -j "${archivePath}" "${binaryWithExt}" -d "${destDir}"`, {
      stdio: 'inherit'
    });
  } else {
    execSync(`tar -xzf "${archivePath}" -C "${destDir}" "${binaryWithExt}"`, {
      stdio: 'inherit'
    });
  }

  // Make executable on Unix-like systems
  if (platform !== 'Windows') {
    fs.chmodSync(destPath, 0o755);
  }

  return destPath;
}

export async function install(binaryName, version, baseDir) {
  console.log(`Installing ${binaryName} v${version}...`);

  const { platform, arch } = getPlatformInfo();
  console.log(`Platform: ${platform} ${arch}`);

  const url = getDownloadUrl(version, platform, arch);
  const archiveExt = platform === 'Windows' ? 'zip' : 'tar.gz';
  const archivePath = path.join(baseDir, `archive.${archiveExt}`);

  console.log('Downloading binary from GitHub Releases...');
  await downloadFile(url, archivePath);
  console.log('✓ Download complete');

  console.log('Extracting binary...');
  extractBinary(archivePath, platform, binaryName, baseDir);
  console.log(`✓ Extracted ${binaryName}`);

  // Clean up archive
  fs.unlinkSync(archivePath);

  console.log(`✓ ${binaryName} installed successfully!`);
}
