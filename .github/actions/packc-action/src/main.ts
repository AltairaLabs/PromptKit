import * as core from '@actions/core';
import { installPackC, installORAS, installCosign } from './installer';
import { compile, validate, parsePackFile, CompileInputs } from './compiler';
import { publish, logout, PublishInputs } from './publisher';
import { sign, SignInputs } from './signer';
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

async function run(): Promise<void> {
  try {
    const inputs = getInputs();

    core.info('PackC Action starting...');
    core.info(`Config file: ${inputs.configFile}`);
    core.info(`Working directory: ${inputs.workingDirectory}`);
    if (inputs.packId) {
      core.info(`Pack ID: ${inputs.packId}`);
    }
    if (inputs.registry) {
      core.info(`Registry: ${inputs.registry}/${inputs.repository}`);
    }

    // Step 1: Install tools
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

    // Step 2: Compile pack
    core.startGroup('Compiling Pack');
    const compileInputs: CompileInputs = {
      configFile: inputs.configFile,
      packId: inputs.packId,
      output: inputs.output,
      workingDirectory: inputs.workingDirectory,
    };
    let compileResult = await compile(compileInputs);
    core.endGroup();

    // Step 3: Validate if enabled
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

    // Step 4: Publish to registry if configured
    let publishResult;
    if (inputs.registry && inputs.repository) {
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
      publishResult = await publish(publishInputs);
      core.endGroup();
    }

    // Step 5: Sign if configured
    let signResult;
    if (inputs.sign && inputs.cosignKey && publishResult) {
      core.startGroup('Signing Pack');
      const signInputs: SignInputs = {
        registryUrl: publishResult.registryUrl,
        digest: publishResult.digest,
        cosignKey: inputs.cosignKey,
        cosignPassword: inputs.cosignPassword,
      };
      signResult = await sign(signInputs);
      core.endGroup();
    }

    // Step 6: Set outputs and log summary
    setOutputs(compileResult, publishResult, signResult);
    logSummary(compileResult, publishResult, signResult);

    // Cleanup: Logout from registry
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
