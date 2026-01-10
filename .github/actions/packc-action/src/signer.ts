import * as core from '@actions/core';
import * as exec from '@actions/exec';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as os from 'node:os';

export interface SignInputs {
  registryUrl: string;
  digest: string;
  cosignKey: string;
  cosignPassword?: string;
}

export interface SignResult {
  signature: string;
}

export async function sign(inputs: SignInputs): Promise<SignResult> {
  const imageRef = inputs.digest
    ? `${inputs.registryUrl.split(':')[0]}@${inputs.digest}`
    : inputs.registryUrl;

  core.info(`Signing: ${imageRef}`);

  // Write key to temp file if it's the key content (not a path)
  let keyPath = inputs.cosignKey;
  let tempKeyFile: string | null = null;

  if (inputs.cosignKey.includes('PRIVATE KEY')) {
    // It's the key content, write to temp file
    tempKeyFile = path.join(os.tmpdir(), `cosign-key-${Date.now()}.key`);
    fs.writeFileSync(tempKeyFile, inputs.cosignKey, { mode: 0o600 });
    keyPath = tempKeyFile;
  }

  try {
    const args = ['sign', '--key', keyPath, imageRef, '--yes'];

    const env: Record<string, string> = {
      ...process.env as Record<string, string>,
    };

    if (inputs.cosignPassword) {
      env.COSIGN_PASSWORD = inputs.cosignPassword;
    }

    let stdout = '';
    let stderr = '';

    const options: exec.ExecOptions = {
      ignoreReturnCode: true,
      env,
      listeners: {
        stdout: (data: Buffer) => {
          stdout += data.toString();
        },
        stderr: (data: Buffer) => {
          stderr += data.toString();
        },
      },
    };

    const exitCode = await exec.exec('cosign', args, options);

    if (stdout) {
      core.info('--- stdout ---');
      core.info(stdout);
    }
    if (stderr) {
      core.info('--- stderr ---');
      core.info(stderr);
    }

    if (exitCode !== 0) {
      throw new Error(`Cosign signing failed: ${stderr}`);
    }

    // Parse signature reference from output
    const signature = parseSignatureRef(stdout + stderr, imageRef);

    core.info('Successfully signed the pack');

    return {
      signature,
    };
  } finally {
    // Clean up temp key file
    if (tempKeyFile && fs.existsSync(tempKeyFile)) {
      fs.unlinkSync(tempKeyFile);
    }
  }
}

function parseSignatureRef(output: string, imageRef: string): string {
  // Cosign creates a signature at: <image>:<tag>.sig or <image>-<digest>.sig
  // Try to find it in the output

  // Pattern 1: Look for "Signature written to" or similar
  const sigRegex = /signature.*?(sha256:[a-f0-9]{64})/i;
  const sigMatch = sigRegex.exec(output);
  if (sigMatch) {
    return sigMatch[1];
  }

  // Pattern 2: Construct expected signature reference
  if (imageRef.includes('@sha256:')) {
    const [base, digest] = imageRef.split('@');
    const sigTag = digest.replace('sha256:', 'sha256-') + '.sig';
    return `${base}:${sigTag}`;
  }

  // Default: return the image ref with .sig suffix
  return `${imageRef}.sig`;
}

export async function verify(
  imageRef: string,
  publicKey: string
): Promise<boolean> {
  core.info(`Verifying signature for: ${imageRef}`);

  // Write public key to temp file if it's the key content
  let keyPath = publicKey;
  let tempKeyFile: string | null = null;

  if (publicKey.includes('PUBLIC KEY')) {
    tempKeyFile = path.join(os.tmpdir(), `cosign-pub-${Date.now()}.pub`);
    fs.writeFileSync(tempKeyFile, publicKey, { mode: 0o644 });
    keyPath = tempKeyFile;
  }

  try {
    const args = ['verify', '--key', keyPath, imageRef];

    const exitCode = await exec.exec('cosign', args, {
      ignoreReturnCode: true,
    });

    return exitCode === 0;
  } finally {
    if (tempKeyFile && fs.existsSync(tempKeyFile)) {
      fs.unlinkSync(tempKeyFile);
    }
  }
}
