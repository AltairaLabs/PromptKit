import * as core from '@actions/core';
import * as exec from '@actions/exec';

export interface PublishInputs {
  packFile: string;
  packId: string;
  version: string;
  registry: string;
  repository: string;
  username?: string;
  password?: string;
}

export interface PublishResult {
  registryUrl: string;
  digest: string;
  tags: string[];
}

const PACK_MEDIA_TYPE = 'application/vnd.promptkit.pack.v1+json';

export async function login(
  registry: string,
  username: string,
  password: string
): Promise<void> {
  core.info(`Logging into registry: ${registry}`);

  const args = ['login', registry, '-u', username, '--password-stdin'];

  let stderr = '';

  const options: exec.ExecOptions = {
    input: Buffer.from(password),
    ignoreReturnCode: true,
    listeners: {
      stderr: (data: Buffer) => {
        stderr += data.toString();
      },
    },
  };

  const exitCode = await exec.exec('oras', args, options);

  if (exitCode !== 0) {
    throw new Error(`Failed to login to registry: ${stderr}`);
  }

  core.info('Successfully logged into registry');
}

export async function publish(inputs: PublishInputs): Promise<PublishResult> {
  // Login if credentials provided
  if (inputs.username && inputs.password) {
    await login(inputs.registry, inputs.username, inputs.password);
  }

  const fullRef = `${inputs.registry}/${inputs.repository}:${inputs.version}`;
  core.info(`Publishing pack to: ${fullRef}`);

  const args = [
    'push',
    fullRef,
    `${inputs.packFile}:${PACK_MEDIA_TYPE}`,
    '--annotation',
    `org.opencontainers.image.title=${inputs.packId}`,
    '--annotation',
    `org.opencontainers.image.version=${inputs.version}`,
    '--annotation',
    `org.opencontainers.image.description=PromptKit pack: ${inputs.packId}`,
  ];

  let stdout = '';
  let stderr = '';

  const options: exec.ExecOptions = {
    ignoreReturnCode: true,
    listeners: {
      stdout: (data: Buffer) => {
        stdout += data.toString();
      },
      stderr: (data: Buffer) => {
        stderr += data.toString();
      },
    },
  };

  const exitCode = await exec.exec('oras', args, options);

  if (stdout) {
    core.info('--- stdout ---');
    core.info(stdout);
  }
  if (stderr) {
    core.info('--- stderr ---');
    core.info(stderr);
  }

  if (exitCode !== 0) {
    throw new Error(`Failed to publish pack: ${stderr}`);
  }

  // Parse digest from output
  // ORAS outputs: "Pushed [registry]/[repo]@sha256:..."
  const digest = parseDigest(stdout + stderr);

  // Also tag as 'latest' if version is a semver
  const tags = [inputs.version];
  if (inputs.version !== 'latest' && /^v?\d+\.\d+\.\d+/.test(inputs.version)) {
    try {
      const latestRef = `${inputs.registry}/${inputs.repository}:latest`;
      await tagImage(fullRef, latestRef);
      tags.push('latest');
    } catch (error) {
      core.warning(`Failed to tag as latest: ${error}`);
    }
  }

  return {
    registryUrl: fullRef,
    digest,
    tags,
  };
}

async function tagImage(source: string, target: string): Promise<void> {
  core.info(`Tagging ${source} as ${target}`);

  const args = ['tag', source, target];

  const exitCode = await exec.exec('oras', args, { ignoreReturnCode: true });

  if (exitCode !== 0) {
    throw new Error(`Failed to tag image`);
  }
}

function parseDigest(output: string): string {
  // Look for sha256 digest in output
  // Pattern: sha256:abc123...
  const digestMatch = output.match(/sha256:[a-f0-9]{64}/i);
  if (digestMatch) {
    return digestMatch[0];
  }

  // Alternative pattern: @sha256:...
  const altMatch = output.match(/@(sha256:[a-f0-9]{64})/i);
  if (altMatch) {
    return altMatch[1];
  }

  return '';
}

export async function logout(registry: string): Promise<void> {
  core.info(`Logging out of registry: ${registry}`);

  const exitCode = await exec.exec('oras', ['logout', registry], {
    ignoreReturnCode: true,
  });

  if (exitCode !== 0) {
    core.warning('Failed to logout from registry');
  }
}
