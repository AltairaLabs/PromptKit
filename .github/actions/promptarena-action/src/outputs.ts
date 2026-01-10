import * as core from '@actions/core';
import * as fs from 'node:fs';
import * as path from 'node:path';

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

function createEmptyResults(): TestResults {
  return {
    passed: 0,
    failed: 0,
    errors: 0,
    total: 0,
    totalCost: 0,
    success: false,
  };
}

function parseIndexJson(outputDir: string): TestResults | null {
  const indexPath = path.join(outputDir, 'index.json');
  if (!fs.existsSync(indexPath)) {
    return null;
  }

  try {
    const content = fs.readFileSync(indexPath, 'utf-8');
    const indexOutput = JSON.parse(content) as ArenaIndexOutput;
    core.info(`Parsed results from ${indexPath}`);

    const total = indexOutput.total_runs ?? 0;
    const passed = indexOutput.successful ?? 0;
    const errors = indexOutput.errors ?? 0;
    const failed = Math.max(0, total - passed - errors);

    return {
      passed,
      failed,
      errors,
      total,
      totalCost: indexOutput.total_cost ?? 0,
      success: failed === 0 && errors === 0 && total > 0,
    };
  } catch (error) {
    core.warning(`Failed to parse ${indexPath}: ${error}`);
    return null;
  }
}

function tryParseJsonFiles(outputDir: string): ArenaOutput | null {
  const jsonFiles = ['results.json', 'output.json', 'arena-results.json'];

  for (const file of jsonFiles) {
    const jsonPath = path.join(outputDir, file);
    if (!fs.existsSync(jsonPath)) {
      continue;
    }

    try {
      const content = fs.readFileSync(jsonPath, 'utf-8');
      const arenaOutput = JSON.parse(content) as ArenaOutput;
      core.info(`Parsed results from ${jsonPath}`);
      return arenaOutput;
    } catch (error) {
      core.warning(`Failed to parse ${jsonPath}: ${error}`);
    }
  }

  return null;
}

function parseFromSummary(summary: ArenaOutput['summary']): TestResults {
  return {
    passed: summary?.passed ?? 0,
    failed: summary?.failed ?? 0,
    errors: summary?.errors ?? 0,
    total: summary?.total ?? 0,
    totalCost: summary?.total_cost ?? 0,
    success: (summary?.failed ?? 0) === 0 && (summary?.errors ?? 0) === 0,
  };
}

function aggregateResults(results: ArenaResult[]): TestResults {
  let passed = 0;
  let failed = 0;
  let errors = 0;
  let totalCost = 0;

  for (const result of results) {
    if (result.status === 'passed') passed++;
    else if (result.status === 'failed') failed++;
    else if (result.status === 'error') errors++;

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

export async function parseResults(outputDir: string): Promise<TestResults> {
  // First, try to parse index.json (Arena's primary output format)
  const indexResults = parseIndexJson(outputDir);
  if (indexResults) {
    return indexResults;
  }

  // Fallback: Try other JSON file formats
  const arenaOutput = tryParseJsonFiles(outputDir);

  // If we have summary data, use it
  if (arenaOutput?.summary) {
    return parseFromSummary(arenaOutput.summary);
  }

  // If we have individual results, aggregate them
  if (arenaOutput?.results && Array.isArray(arenaOutput.results)) {
    return aggregateResults(arenaOutput.results);
  }

  // No results found - return empty results
  core.warning('No results file found or results could not be parsed');
  return createEmptyResults();
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
