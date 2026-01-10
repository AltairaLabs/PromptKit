import * as core from '@actions/core';
import * as exec from '@actions/exec';
import * as fs from 'node:fs';
import * as path from 'node:path';

export interface CompileInputs {
  configFile: string;
  packId?: string;
  output?: string;
  workingDirectory: string;
}

export interface CompileResult {
  packFile: string;
  packId: string;
  prompts: number;
  tools: number;
  exitCode: number;
}

export async function compile(inputs: CompileInputs): Promise<CompileResult> {
  const args: string[] = ['compile'];

  args.push('--config', inputs.configFile);

  if (inputs.packId) {
    args.push('--id', inputs.packId);
  }

  if (inputs.output) {
    args.push('--output', inputs.output);
  }

  core.info(`Running: packc ${args.join(' ')}`);

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

  const exitCode = await exec.exec('packc', args, options);

  if (stdout) {
    core.info('--- stdout ---');
    core.info(stdout);
  }
  if (stderr) {
    core.info('--- stderr ---');
    core.info(stderr);
  }

  if (exitCode !== 0) {
    throw new Error(`packc compile failed with exit code ${exitCode}`);
  }

  // Parse output to extract pack info
  const result = parseCompileOutput(stdout + stderr, inputs);

  return {
    ...result,
    exitCode,
  };
}

function parseCompileOutput(output: string, inputs: CompileInputs): Omit<CompileResult, 'exitCode'> {
  // Extract pack file path from output
  // Example: "✓ Pack compiled successfully: my-pack.pack.json"
  let packFile = inputs.output || '';
  const packFileRegex = /Pack compiled successfully:?\s*([^\s]+\.pack\.json)/i;
  const packFileMatch = packFileRegex.exec(output);
  if (packFileMatch) {
    packFile = packFileMatch[1];
  }

  // If still empty, try to derive from pack-id
  if (!packFile && inputs.packId) {
    packFile = `${inputs.packId}.pack.json`;
  }

  // Make path absolute if needed
  if (packFile && !path.isAbsolute(packFile)) {
    packFile = path.join(inputs.workingDirectory, packFile);
  }

  // Extract pack ID
  // Example: "Compiling 3 prompts into pack 'my-pack'..."
  let packId = inputs.packId || '';
  const packIdRegex = /into pack ['"]?([^'"]+)['"]?/i;
  const packIdMatch = packIdRegex.exec(output);
  if (packIdMatch) {
    packId = packIdMatch[1];
  }

  // Extract prompts count
  // Example: "Compiling 3 prompts into pack"
  let prompts = 0;
  const promptsRegex = /Compiling (\d+) prompts?/i;
  const promptsMatch = promptsRegex.exec(output);
  if (promptsMatch) {
    prompts = Number.parseInt(promptsMatch[1], 10);
  }

  // Extract tools count
  // Example: "Including 2 tool definitions in pack"
  let tools = 0;
  const toolsRegex = /Including (\d+) tool/i;
  const toolsMatch = toolsRegex.exec(output);
  if (toolsMatch) {
    tools = Number.parseInt(toolsMatch[1], 10);
  }

  // Also try to parse from "Contains X prompts" line
  const containsPromptsRegex = /Contains (\d+) prompts?/i;
  const containsPromptsMatch = containsPromptsRegex.exec(output);
  if (containsPromptsMatch && prompts === 0) {
    prompts = Number.parseInt(containsPromptsMatch[1], 10);
  }

  const containsToolsRegex = /Contains (\d+) tools?/i;
  const containsToolsMatch = containsToolsRegex.exec(output);
  if (containsToolsMatch && tools === 0) {
    tools = Number.parseInt(containsToolsMatch[1], 10);
  }

  return {
    packFile,
    packId,
    prompts,
    tools,
  };
}

export async function validate(packFile: string): Promise<boolean> {
  core.info(`Validating pack: ${packFile}`);

  if (!fs.existsSync(packFile)) {
    throw new Error(`Pack file not found: ${packFile}`);
  }

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

  const exitCode = await exec.exec('packc', ['validate', packFile], options);

  if (stdout) {
    core.info(stdout);
  }
  if (stderr) {
    core.info(stderr);
  }

  if (exitCode !== 0) {
    core.warning(`Pack validation failed with exit code ${exitCode}`);
    return false;
  }

  core.info('Pack validation passed');
  return true;
}

export function parsePackFile(packFile: string): { prompts: number; tools: number; packId: string } {
  if (!fs.existsSync(packFile)) {
    return { prompts: 0, tools: 0, packId: '' };
  }

  try {
    const content = fs.readFileSync(packFile, 'utf-8');
    const pack = JSON.parse(content) as {
      id?: string;
      prompts?: Record<string, unknown>;
      tools?: Record<string, unknown>;
    };

    return {
      packId: pack.id || '',
      prompts: pack.prompts ? Object.keys(pack.prompts).length : 0,
      tools: pack.tools ? Object.keys(pack.tools).length : 0,
    };
  } catch {
    return { prompts: 0, tools: 0, packId: '' };
  }
}
