import * as core from '@actions/core';
import { installPromptArena } from './installer';
import { runPromptArena, getOutputPaths, RunnerInputs } from './runner';
import { parseResults, setOutputs, logSummary } from './outputs';
import * as path from 'path';

async function run(): Promise<void> {
  try {
    // Get inputs
    const configFile = core.getInput('config-file', { required: true });
    const version = core.getInput('version') || 'latest';
    const scenarios = core.getInput('scenarios');
    const providers = core.getInput('providers');
    const regions = core.getInput('regions');
    const outputDir = core.getInput('output-dir') || 'out';
    const junitOutput = core.getInput('junit-output');
    const failOnError = core.getInput('fail-on-error') !== 'false';
    const workingDirectory = core.getInput('working-directory') || '.';

    core.info('PromptArena Action starting...');
    core.info(`Config file: ${configFile}`);
    core.info(`Version: ${version}`);
    core.info(`Working directory: ${workingDirectory}`);

    // Install promptarena
    core.startGroup('Installing PromptArena');
    const binaryPath = await installPromptArena(version);
    core.info(`Installed at: ${binaryPath}`);
    core.endGroup();

    // Run tests
    core.startGroup('Running PromptArena Tests');
    const runnerInputs: RunnerInputs = {
      configFile,
      scenarios: scenarios || undefined,
      providers: providers || undefined,
      regions: regions || undefined,
      outputDir,
      junitOutput: junitOutput || undefined,
      workingDirectory,
    };

    const runResult = await runPromptArena(runnerInputs);
    core.endGroup();

    // Get output paths
    const outputPaths = getOutputPaths(workingDirectory, outputDir, junitOutput);

    // Parse results
    core.startGroup('Processing Results');
    const fullOutputDir = path.join(workingDirectory, outputDir);
    const results = await parseResults(fullOutputDir);
    logSummary(results);
    core.endGroup();

    // Set outputs
    setOutputs(results, outputPaths.junitPath, outputPaths.htmlPath);

    // Handle failure
    if (runResult.exitCode !== 0 || !results.success) {
      const message = `PromptArena tests failed: ${results.failed} failures, ${results.errors} errors`;

      if (failOnError) {
        core.setFailed(message);
      } else {
        core.warning(message);
      }
    } else {
      core.info('All PromptArena tests passed!');
    }
  } catch (error) {
    if (error instanceof Error) {
      core.setFailed(error.message);
    } else {
      core.setFailed('An unexpected error occurred');
    }
  }
}

run();
