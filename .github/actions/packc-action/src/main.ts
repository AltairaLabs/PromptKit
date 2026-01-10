import * as core from '@actions/core';
import { installPackC, installORAS, installCosign } from './installer';
import { compile, validate, parsePackFile, CompileInputs, CompileResult } from './compiler';
import { publish, logout, PublishInputs, PublishResult } from './publisher';
import { sign, SignInputs, SignResult } from './signer';
import { setOutputs, logSummary } from './outputs';

interface ActionInputs {
  configFile: string;
  packId?: string;
  version: string;
  packcVersion: string;
  output?: string;
  validate: boolean;
  registry?: string;
  repository?: string;
  username?: string;
  password?: string;
  sign: boolean;
  cosignKey?: string;
  cosignPassword?: string;
  workingDirectory: string;
}

function getInputs(): ActionInputs {
  return {
    configFile: core.getInput('config-file', { required: true }),
    packId: core.getInput('pack-id') || undefined,
    version: core.getInput('version') || 'latest',
    packcVersion: core.getInput('packc-version') || 'latest',
    output: core.getInput('output') || undefined,
    validate: core.getInput('validate') !== 'false',
    registry: core.getInput('registry') || undefined,
    repository: core.getInput('repository') || undefined,
    username: core.getInput('username') || undefined,
    password: core.getInput('password') || undefined,
    sign: core.getInput('sign') === 'true',
    cosignKey: core.getInput('cosign-key') || undefined,
    cosignPassword: core.getInput('cosign-password') || undefined,
    workingDirectory: core.getInput('working-directory') || '.',
  };
}

function logActionStart(inputs: ActionInputs): void {
  core.info('PackC Action starting...');
  core.info(`Config file: ${inputs.configFile}`);
  core.info(`Working directory: ${inputs.workingDirectory}`);
  if (inputs.packId) {
    core.info(`Pack ID: ${inputs.packId}`);
  }
  if (inputs.registry) {
    core.info(`Registry: ${inputs.registry}/${inputs.repository}`);
  }
}

async function installTools(inputs: ActionInputs): Promise<void> {
  core.startGroup('Installing PackC');
  await installPackC(inputs.packcVersion);
  core.endGroup();

  if (inputs.registry) {
    core.startGroup('Installing ORAS');
    await installORAS();
    core.endGroup();
  }

  if (inputs.sign) {
    core.startGroup('Installing Cosign');
    await installCosign();
    core.endGroup();
  }
}

async function compilePack(inputs: ActionInputs): Promise<CompileResult> {
  core.startGroup('Compiling Pack');
  const compileInputs: CompileInputs = {
    configFile: inputs.configFile,
    packId: inputs.packId,
    output: inputs.output,
    workingDirectory: inputs.workingDirectory,
  };
  let compileResult = await compile(compileInputs);
  core.endGroup();

  if (inputs.validate) {
    core.startGroup('Validating Pack');
    const isValid = await validate(compileResult.packFile);
    if (!isValid) {
      core.warning('Pack validation had warnings');
    }
    core.endGroup();
  }

  // Parse pack file for more accurate counts
  const packInfo = parsePackFile(compileResult.packFile);
  if (packInfo.prompts > 0) {
    compileResult = {
      ...compileResult,
      prompts: packInfo.prompts,
      tools: packInfo.tools,
      packId: packInfo.packId || compileResult.packId,
    };
  }

  return compileResult;
}

async function publishPack(
  inputs: ActionInputs,
  compileResult: CompileResult
): Promise<PublishResult | undefined> {
  if (!inputs.registry || !inputs.repository) {
    return undefined;
  }

  core.startGroup('Publishing to Registry');
  const publishInputs: PublishInputs = {
    packFile: compileResult.packFile,
    packId: compileResult.packId,
    version: inputs.version,
    registry: inputs.registry,
    repository: inputs.repository,
    username: inputs.username,
    password: inputs.password,
  };
  const result = await publish(publishInputs);
  core.endGroup();
  return result;
}

async function signPack(
  inputs: ActionInputs,
  publishResult: PublishResult | undefined
): Promise<SignResult | undefined> {
  if (!inputs.sign || !inputs.cosignKey || !publishResult) {
    return undefined;
  }

  core.startGroup('Signing Pack');
  const signInputs: SignInputs = {
    registryUrl: publishResult.registryUrl,
    digest: publishResult.digest,
    cosignKey: inputs.cosignKey,
    cosignPassword: inputs.cosignPassword,
  };
  const result = await sign(signInputs);
  core.endGroup();
  return result;
}

async function run(): Promise<void> {
  try {
    const inputs = getInputs();
    logActionStart(inputs);

    await installTools(inputs);
    const compileResult = await compilePack(inputs);
    const publishResult = await publishPack(inputs, compileResult);
    const signResult = await signPack(inputs, publishResult);

    setOutputs(compileResult, publishResult, signResult);
    logSummary(compileResult, publishResult, signResult);

    if (inputs.registry && inputs.username) {
      await logout(inputs.registry);
    }

    core.info('PackC Action completed successfully!');
  } catch (error) {
    if (error instanceof Error) {
      core.setFailed(error.message);
    } else {
      core.setFailed('An unexpected error occurred');
    }
  }
}

run();
