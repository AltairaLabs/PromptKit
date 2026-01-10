import * as core from '@actions/core';
import * as exec from '@actions/exec';
import * as path from 'path';

export interface RunnerInputs {
  configFile: string;
  scenarios?: string;
  providers?: string;
  regions?: string;
  outputDir: string;
  junitOutput?: string;
  workingDirectory: string;
}

export interface RunResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

export async function runPromptArena(inputs: RunnerInputs): Promise<RunResult> {
  const args: string[] = ['run'];

  // Required config file
  args.push('--config', inputs.configFile);

  // CI mode for non-interactive output
  args.push('--ci');

  // Output formats - always include json and junit for parsing
  const formats = ['json', 'junit'];
  args.push('--format', formats.join(','));

  // Output directory
  args.push('--out', inputs.outputDir);

  // Optional JUnit output path
  if (inputs.junitOutput) {
    args.push('--junit-file', inputs.junitOutput);
  }

  // Optional filters
  if (inputs.scenarios) {
    const scenarioList = inputs.scenarios.split(',').map((s) => s.trim());
    for (const scenario of scenarioList) {
      args.push('--scenario', scenario);
    }
  }

  if (inputs.providers) {
    const providerList = inputs.providers.split(',').map((p) => p.trim());
    for (const provider of providerList) {
      args.push('--provider', provider);
    }
  }

  if (inputs.regions) {
    const regionList = inputs.regions.split(',').map((r) => r.trim());
    for (const region of regionList) {
      args.push('--region', region);
    }
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
