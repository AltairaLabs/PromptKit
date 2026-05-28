import * as core from '@actions/core';
import * as exec from '@actions/exec';
import * as path from 'node:path';

export interface RunnerInputs {
  configFile: string;
  scenarios?: string;
  providers?: string;
  regions?: string;
  outputDir: string;
  formats?: string;
  junitOutput?: string;
  overrideProviders?: string;
  workingDirectory: string;
}

export interface RunResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

export async function runPromptArena(inputs: RunnerInputs): Promise<RunResult> {
  // Initialize args with required options
  const configuredFormats = inputs.formats
    ?.split(',')
    .map((format) => format.trim())
    .filter(Boolean);
  const formats = configuredFormats && configuredFormats.length > 0
    ? configuredFormats
    : ['json', 'junit'];
  const args: string[] = [
    'run',
    '--config', inputs.configFile,
    '--ci',
    '--format', formats.join(','),
    '--out', inputs.outputDir,
  ];

  // Optional JUnit output path
  if (inputs.junitOutput) {
    args.push('--junit-file', inputs.junitOutput);
  }

  // Optional filters
  if (inputs.scenarios) {
    const scenarioArgs = inputs.scenarios
      .split(',')
      .map((s) => s.trim())
      .flatMap((scenario) => ['--scenario', scenario]);
    args.push(...scenarioArgs);
  }

  if (inputs.providers) {
    const providerArgs = inputs.providers
      .split(',')
      .map((p) => p.trim())
      .flatMap((provider) => ['--provider', provider]);
    args.push(...providerArgs);
  }

  if (inputs.regions) {
    const regionArgs = inputs.regions
      .split(',')
      .map((r) => r.trim())
      .flatMap((region) => ['--region', region]);
    args.push(...regionArgs);
  }

  if (inputs.overrideProviders) {
    const overrideArgs = inputs.overrideProviders
      .split(',')
      .map((p) => p.trim())
      .filter(Boolean)
      .flatMap((pair) => ['--override-provider', pair]);
    args.push(...overrideArgs);
  }

  core.info(`Running: promptarena ${args.join(' ')}`);

  let stdout = '';
  let stderr = '';

  const options: exec.ExecOptions = {
    cwd: inputs.workingDirectory,
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

  const exitCode = await exec.exec('promptarena', args, options);

  // Log output
  if (stdout) {
    core.info('--- stdout ---');
    core.info(stdout);
  }
  if (stderr) {
    core.info('--- stderr ---');
    core.info(stderr);
  }

  return {
    exitCode,
    stdout,
    stderr,
  };
}

export function getOutputPaths(
  workingDirectory: string,
  outputDir: string,
  junitOutput?: string
): { junitPath: string; htmlPath: string; jsonPath: string } {
  const baseDir = path.join(workingDirectory, outputDir);

  return {
    junitPath: junitOutput || path.join(baseDir, 'junit.xml'),
    htmlPath: path.join(baseDir, 'report.html'),
    jsonPath: path.join(baseDir, 'results.json'),
  };
}
