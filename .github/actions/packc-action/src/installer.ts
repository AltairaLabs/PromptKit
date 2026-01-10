import * as core from '@actions/core';
import * as tc from '@actions/tool-cache';
import * as path from 'node:path';
import * as os from 'node:os';
import * as fs from 'node:fs';

const PROMPTKIT_REPO_OWNER = 'AltairaLabs';
const PROMPTKIT_REPO_NAME = 'PromptKit';
const ORAS_REPO_OWNER = 'oras-project';
const ORAS_REPO_NAME = 'oras';
const COSIGN_REPO_OWNER = 'sigstore';
const COSIGN_REPO_NAME = 'cosign';

interface PlatformInfo {
  os: string;
  arch: string;
  orasOs: string;
  orasArch: string;
}

function getPlatformInfo(): PlatformInfo {
  const platform = os.platform();
  const arch = os.arch();

  let osName: string;
  let orasOs: string;
  switch (platform) {
    case 'linux':
      osName = 'Linux';
      orasOs = 'linux';
      break;
    case 'darwin':
      osName = 'Darwin';
      orasOs = 'darwin';
      break;
    case 'win32':
      osName = 'Windows';
      orasOs = 'windows';
      break;
    default:
      throw new Error(`Unsupported platform: ${platform}`);
  }

  let archName: string;
  let orasArch: string;
  switch (arch) {
    case 'x64':
      archName = 'x86_64';
      orasArch = 'amd64';
      break;
    case 'arm64':
      archName = 'arm64';
      orasArch = 'arm64';
      break;
    default:
      throw new Error(`Unsupported architecture: ${arch}`);
  }

  return { os: osName, arch: archName, orasOs, orasArch };
}

async function getLatestPromptKitVersion(): Promise<string> {
  const response = await fetch(
    `https://api.github.com/repos/${PROMPTKIT_REPO_OWNER}/${PROMPTKIT_REPO_NAME}/releases/latest`,
    {
      headers: {
        Accept: 'application/vnd.github.v3+json',
        'User-Agent': 'packc-action',
      },
    }
  );

  if (!response.ok) {
    throw new Error(`Failed to fetch latest release: ${response.statusText}`);
  }

  const release = (await response.json()) as { tag_name: string };
  return release.tag_name;
}

async function getLatestOrasVersion(): Promise<string> {
  const response = await fetch(
    `https://api.github.com/repos/${ORAS_REPO_OWNER}/${ORAS_REPO_NAME}/releases/latest`,
    {
      headers: {
        Accept: 'application/vnd.github.v3+json',
        'User-Agent': 'packc-action',
      },
    }
  );

  if (!response.ok) {
    throw new Error(`Failed to fetch latest ORAS release: ${response.statusText}`);
  }

  const release = (await response.json()) as { tag_name: string };
  return release.tag_name;
}

async function getLatestCosignVersion(): Promise<string> {
  const response = await fetch(
    `https://api.github.com/repos/${COSIGN_REPO_OWNER}/${COSIGN_REPO_NAME}/releases/latest`,
    {
      headers: {
        Accept: 'application/vnd.github.v3+json',
        'User-Agent': 'packc-action',
      },
    }
  );

  if (!response.ok) {
    throw new Error(`Failed to fetch latest Cosign release: ${response.statusText}`);
  }

  const release = (await response.json()) as { tag_name: string };
  return release.tag_name;
}

export async function installPackC(version: string): Promise<string> {
  const platformInfo = getPlatformInfo();

  let resolvedVersion = version;
  if (version === 'latest') {
    core.info('Resolving latest PackC version...');
    resolvedVersion = await getLatestPromptKitVersion();
    core.info(`Latest version is ${resolvedVersion}`);
  }

  if (!resolvedVersion.startsWith('v')) {
    resolvedVersion = `v${resolvedVersion}`;
  }

  // Check cache first
  let toolPath = tc.find('packc', resolvedVersion, platformInfo.arch);

  if (toolPath) {
    core.info(`Found cached packc ${resolvedVersion}`);
  } else {
    core.info(`Downloading packc ${resolvedVersion}...`);

    const versionNumber = resolvedVersion.startsWith('v') ? resolvedVersion.slice(1) : resolvedVersion;
    const assetName = `PromptKit_${versionNumber}_${platformInfo.os}_${platformInfo.arch}.tar.gz`;
    const downloadUrl = `https://github.com/${PROMPTKIT_REPO_OWNER}/${PROMPTKIT_REPO_NAME}/releases/download/${resolvedVersion}/${assetName}`;

    core.info(`Download URL: ${downloadUrl}`);

    const archivePath = await tc.downloadTool(downloadUrl);
    const extractedPath = await tc.extractTar(archivePath);

    const binaryName = platformInfo.os === 'Windows' ? 'packc.exe' : 'packc';
    const binaryPath = path.join(extractedPath, binaryName);

    if (!fs.existsSync(binaryPath)) {
      throw new Error(`packc binary not found at ${binaryPath}`);
    }

    if (platformInfo.os !== 'Windows') {
      fs.chmodSync(binaryPath, 0o755);
    }

    toolPath = await tc.cacheDir(extractedPath, 'packc', resolvedVersion, platformInfo.arch);
    core.info(`Cached packc to ${toolPath}`);
  }

  core.addPath(toolPath);
  core.info(`Added ${toolPath} to PATH`);

  return path.join(toolPath, platformInfo.os === 'Windows' ? 'packc.exe' : 'packc');
}

export async function installORAS(): Promise<string> {
  const platformInfo = getPlatformInfo();

  core.info('Resolving latest ORAS version...');
  const version = await getLatestOrasVersion();
  core.info(`Latest ORAS version is ${version}`);

  const versionNumber = version.startsWith('v') ? version.slice(1) : version;

  // Check cache first
  let toolPath = tc.find('oras', version, platformInfo.orasArch);

  if (toolPath) {
    core.info(`Found cached ORAS ${version}`);
  } else {
    core.info(`Downloading ORAS ${version}...`);

    const ext = platformInfo.os === 'Windows' ? 'zip' : 'tar.gz';
    const assetName = `oras_${versionNumber}_${platformInfo.orasOs}_${platformInfo.orasArch}.${ext}`;
    const downloadUrl = `https://github.com/${ORAS_REPO_OWNER}/${ORAS_REPO_NAME}/releases/download/${version}/${assetName}`;

    core.info(`Download URL: ${downloadUrl}`);

    const archivePath = await tc.downloadTool(downloadUrl);
    let extractedPath: string;

    if (platformInfo.os === 'Windows') {
      extractedPath = await tc.extractZip(archivePath);
    } else {
      extractedPath = await tc.extractTar(archivePath);
    }

    const binaryName = platformInfo.os === 'Windows' ? 'oras.exe' : 'oras';
    const binaryPath = path.join(extractedPath, binaryName);

    if (!fs.existsSync(binaryPath)) {
      throw new Error(`ORAS binary not found at ${binaryPath}`);
    }

    if (platformInfo.os !== 'Windows') {
      fs.chmodSync(binaryPath, 0o755);
    }

    toolPath = await tc.cacheDir(extractedPath, 'oras', version, platformInfo.orasArch);
    core.info(`Cached ORAS to ${toolPath}`);
  }

  core.addPath(toolPath);
  core.info(`Added ORAS ${toolPath} to PATH`);

  return path.join(toolPath, platformInfo.os === 'Windows' ? 'oras.exe' : 'oras');
}

export async function installCosign(): Promise<string> {
  const platformInfo = getPlatformInfo();

  core.info('Resolving latest Cosign version...');
  const version = await getLatestCosignVersion();
  core.info(`Latest Cosign version is ${version}`);

  // Check cache first
  let toolPath = tc.find('cosign', version, platformInfo.orasArch);

  if (toolPath) {
    core.info(`Found cached Cosign ${version}`);
  } else {
    core.info(`Downloading Cosign ${version}...`);

    const ext = platformInfo.os === 'Windows' ? '.exe' : '';
    const assetName = `cosign-${platformInfo.orasOs}-${platformInfo.orasArch}${ext}`;
    const downloadUrl = `https://github.com/${COSIGN_REPO_OWNER}/${COSIGN_REPO_NAME}/releases/download/${version}/${assetName}`;

    core.info(`Download URL: ${downloadUrl}`);

    const downloadPath = await tc.downloadTool(downloadUrl);

    // Create a directory for the binary
    const toolDir = path.join(os.tmpdir(), `cosign-${version}`);
    fs.mkdirSync(toolDir, { recursive: true });

    const binaryName = platformInfo.os === 'Windows' ? 'cosign.exe' : 'cosign';
    const binaryPath = path.join(toolDir, binaryName);

    fs.copyFileSync(downloadPath, binaryPath);

    if (platformInfo.os !== 'Windows') {
      fs.chmodSync(binaryPath, 0o755);
    }

    toolPath = await tc.cacheDir(toolDir, 'cosign', version, platformInfo.orasArch);
    core.info(`Cached Cosign to ${toolPath}`);
  }

  core.addPath(toolPath);
  core.info(`Added Cosign ${toolPath} to PATH`);

  return path.join(toolPath, platformInfo.os === 'Windows' ? 'cosign.exe' : 'cosign');
}
