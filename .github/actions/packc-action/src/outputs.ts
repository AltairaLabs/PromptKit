import * as core from '@actions/core';
import { CompileResult } from './compiler';
import { PublishResult } from './publisher';
import { SignResult } from './signer';

export function setOutputs(
  compileResult: CompileResult,
  publishResult?: PublishResult,
  signResult?: SignResult
): void {
  // Compile outputs
  core.setOutput('pack-file', compileResult.packFile);
  core.setOutput('pack-id', compileResult.packId);
  core.setOutput('prompts', compileResult.prompts.toString());
  core.setOutput('tools', compileResult.tools.toString());

  // Publish outputs
  if (publishResult) {
    core.setOutput('registry-url', publishResult.registryUrl);
    core.setOutput('digest', publishResult.digest);
  }

  // Sign outputs
  if (signResult) {
    core.setOutput('signature', signResult.signature);
  }
}

export function logSummary(
  compileResult: CompileResult,
  publishResult?: PublishResult,
  signResult?: SignResult
): void {
  core.info('');
  core.info('=== PackC Action Summary ===');
  core.info(`Pack ID:     ${compileResult.packId}`);
  core.info(`Pack File:   ${compileResult.packFile}`);
  core.info(`Prompts:     ${compileResult.prompts}`);
  core.info(`Tools:       ${compileResult.tools}`);

  if (publishResult) {
    core.info('');
    core.info('--- Published ---');
    core.info(`Registry:    ${publishResult.registryUrl}`);
    core.info(`Digest:      ${publishResult.digest}`);
    core.info(`Tags:        ${publishResult.tags.join(', ')}`);
  }

  if (signResult) {
    core.info('');
    core.info('--- Signed ---');
    core.info(`Signature:   ${signResult.signature}`);
  }

  core.info('============================');
  core.info('');
}
