import * as core from '@actions/core';
import * as tc from '@actions/tool-cache';
import * as path from 'path';
import * as os from 'os';
import * as fs from 'fs';

const REPO_OWNER = 'AltairaLabs';
const REPO_NAME = 'PromptKit';
const TOOL_NAME = 'promptarena';

interface PlatformInfo {
  os: string;
  arch: string;
}

function getPlatformInfo(): PlatformInfo {
  const platform = os.platform();
  const arch = os.arch();

  let osName: string;
  switch (platform) {
    case 'linux':
      osName = 'Linux';
      break;
    case 'darwin':
      osName = 'Darwin';
      break;
    case 'win32':
      osName = 'Windows';
      break;
    default:
      throw new Error(`Unsupported platform: ${platform}`);
  }

  let archName: string;
  switch (arch) {
    case 'x64':
      archName = 'x86_64';
      break;
    case 'arm64':
      archName = 'arm64';
      break;
    default:
      throw new Error(`Unsupported architecture: ${arch}`);
  }

  return { os: osName, arch: archName };
}

function getAssetName(version: string, platformInfo: PlatformInfo): string {
  // Remove 'v' prefix if present for asset name
  const versionNumber = version.startsWith('v') ? version.slice(1) : version;
  return `PromptKit_${versionNumber}_${platformInfo.os}_${platformInfo.arch}.tar.gz`;
}

function getDownloadUrl(version: string, assetName: string): string {
  return `https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${assetName}`;
}

async function getLatestVersion(): Promise<string> {
  const response = await fetch(
    `https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest`,
    {
      headers: {
        Accept: 'application/vnd.github.v3+json',
        'User-Agent': 'promptarena-action',
      },
    }
  );

  if (!response.ok) {
    throw new Error(`Failed to fetch latest release: ${response.statusText}`);
  }

  const release = (await response.json()) as { tag_name: string };
  return release.tag_name;
}

export async function installPromptArena(version: string): Promise<string> {
  const platformInfo = getPlatformInfo();

  // Resolve 'latest' to actual version
  let resolvedVersion = version;
  if (version === 'latest') {
    core.info('Resolving latest version...');
    resolvedVersion = await getLatestVersion();
    core.info(`Latest version is ${resolvedVersion}`);
  }

  // Ensure version has 'v' prefix
  if (!resolvedVersion.startsWith('v')) {
    resolvedVersion = `v${resolvedVersion}`;
  }

  // Check cache first
  let toolPath = tc.find(TOOL_NAME, resolvedVersion, platformInfo.arch);

  if (toolPath) {
    core.info(`Found cached promptarena ${resolvedVersion}`);
  } else {
    core.info(`Downloading promptarena ${resolvedVersion}...`);

    const assetName = getAssetName(resolvedVersion, platformInfo);
    const downloadUrl = getDownloadUrl(resolvedVersion, assetName);

    core.info(`Download URL: ${downloadUrl}`);

    // Download the archive
    const archivePath = await tc.downloadTool(downloadUrl);

    // Extract the archive
    core.info('Extracting archive...');
    const extractedPath = await tc.extractTar(archivePath);

    // Find the binary in the extracted directory
    const binaryName = platformInfo.os === 'Windows' ? 'promptarena.exe' : 'promptarena';
    const binaryPath = path.join(extractedPath, binaryName);

    if (!fs.existsSync(binaryPath)) {
      throw new Error(`Binary not found at ${binaryPath}`);
    }

    // Make binary executable on Unix systems
    if (platformInfo.os !== 'Windows') {
      fs.chmodSync(binaryPath, 0o755);
    }

    // Cache the tool
    toolPath = await tc.cacheDir(extractedPath, TOOL_NAME, resolvedVersion, platformInfo.arch);
    core.info(`Cached promptarena to ${toolPath}`);
  }

  // Add to PATH
  core.addPath(toolPath);
  core.info(`Added ${toolPath} to PATH`);

  // Verify installation
  const binaryName = platformInfo.os === 'Windows' ? 'promptarena.exe' : 'promptarena';
  const fullBinaryPath = path.join(toolPath, binaryName);

  if (!fs.existsSync(fullBinaryPath)) {
    throw new Error(`promptarena binary not found at ${fullBinaryPath}`);
  }

  return fullBinaryPath;
}
