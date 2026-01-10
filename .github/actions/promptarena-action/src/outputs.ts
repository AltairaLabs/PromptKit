import * as core from '@actions/core';
import * as fs from 'fs';
import * as path from 'path';

export interface TestResults {
  passed: number;
  failed: number;
  errors: number;
  total: number;
  totalCost: number;
  success: boolean;
}

interface ArenaResult {
  status: 'passed' | 'failed' | 'error';
  cost?: number;
}

interface ArenaOutput {
  results?: ArenaResult[];
  summary?: {
    passed?: number;
    failed?: number;
    errors?: number;
    total?: number;
    total_cost?: number;
  };
}

// Arena index.json format
interface ArenaIndexOutput {
  successful?: number;
  errors?: number;
  total_runs?: number;
  total_cost?: number;
}

export async function parseResults(outputDir: string): Promise<TestResults> {
  // First, try to parse index.json (Arena's primary output format)
  const indexPath = path.join(outputDir, 'index.json');
  if (fs.existsSync(indexPath)) {
    try {
      const content = fs.readFileSync(indexPath, 'utf-8');
      const indexOutput = JSON.parse(content) as ArenaIndexOutput;
      core.info(`Parsed results from ${indexPath}`);

      const total = indexOutput.total_runs ?? 0;
      const passed = indexOutput.successful ?? 0;
      const errors = indexOutput.errors ?? 0;
      const failed = total - passed - errors;

      return {
        passed,
        failed: failed > 0 ? failed : 0,
        errors,
        total,
        totalCost: indexOutput.total_cost ?? 0,
        success: failed === 0 && errors === 0 && total > 0,
      };
    } catch (error) {
      core.warning(`Failed to parse ${indexPath}: ${error}`);
    }
  }

  // Fallback: Try other JSON file formats
  const jsonFiles = ['results.json', 'output.json', 'arena-results.json'];
  let arenaOutput: ArenaOutput | null = null;

  for (const file of jsonFiles) {
    const jsonPath = path.join(outputDir, file);
    if (fs.existsSync(jsonPath)) {
      try {
        const content = fs.readFileSync(jsonPath, 'utf-8');
        arenaOutput = JSON.parse(content) as ArenaOutput;
        core.info(`Parsed results from ${jsonPath}`);
        break;
      } catch (error) {
        core.warning(`Failed to parse ${jsonPath}: ${error}`);
      }
    }
  }

  // If we have summary data, use it
  if (arenaOutput?.summary) {
    const summary = arenaOutput.summary;
    return {
      passed: summary.passed ?? 0,
      failed: summary.failed ?? 0,
      errors: summary.errors ?? 0,
      total: summary.total ?? 0,
      totalCost: summary.total_cost ?? 0,
      success: (summary.failed ?? 0) === 0 && (summary.errors ?? 0) === 0,
    };
  }

  // If we have individual results, aggregate them
  if (arenaOutput?.results && Array.isArray(arenaOutput.results)) {
    const results = arenaOutput.results;
    let passed = 0;
    let failed = 0;
    let errors = 0;
    let totalCost = 0;

    for (const result of results) {
      switch (result.status) {
        case 'passed':
          passed++;
          break;
        case 'failed':
          failed++;
          break;
        case 'error':
          errors++;
          break;
      }
      if (result.cost) {
        totalCost += result.cost;
      }
    }

    return {
      passed,
      failed,
      errors,
      total: results.length,
      totalCost,
      success: failed === 0 && errors === 0,
    };
  }

  // No results found - return empty results
  core.warning('No results file found or results could not be parsed');
  return {
    passed: 0,
    failed: 0,
    errors: 0,
    total: 0,
    totalCost: 0,
    success: false,
  };
}

export function setOutputs(
  results: TestResults,
  junitPath: string,
  htmlPath: string
): void {
  core.setOutput('passed', results.passed.toString());
  core.setOutput('failed', results.failed.toString());
  core.setOutput('errors', results.errors.toString());
  core.setOutput('total', results.total.toString());
  core.setOutput('total-cost', results.totalCost.toFixed(6));
  core.setOutput('success', results.success.toString());

  // Set file paths if they exist
  if (fs.existsSync(junitPath)) {
    core.setOutput('junit-path', junitPath);
  }
  if (fs.existsSync(htmlPath)) {
    core.setOutput('html-path', htmlPath);
  }
}

export function logSummary(results: TestResults): void {
  core.info('');
  core.info('=== PromptArena Test Results ===');
  core.info(`Total:  ${results.total}`);
  core.info(`Passed: ${results.passed}`);
  core.info(`Failed: ${results.failed}`);
  core.info(`Errors: ${results.errors}`);
  core.info(`Cost:   $${results.totalCost.toFixed(6)}`);
  core.info(`Status: ${results.success ? 'SUCCESS' : 'FAILURE'}`);
  core.info('================================');
  core.info('');
}
