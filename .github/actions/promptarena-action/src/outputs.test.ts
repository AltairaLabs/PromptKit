import * as core from '@actions/core';
import * as fs from 'fs';
import { parseResults, setOutputs, logSummary, TestResults } from './outputs';

jest.mock('fs');

const mockedCore = jest.mocked(core);
const mockedFs = jest.mocked(fs);

describe('outputs', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('parseResults', () => {
    it('should parse index.json format', async () => {
      mockedFs.existsSync.mockImplementation((path: fs.PathLike) =>
        path.toString().includes('index.json')
      );
      mockedFs.readFileSync.mockReturnValue(
        JSON.stringify({
          successful: 5,
          errors: 1,
          total_runs: 8,
          total_cost: 0.002,
        })
      );

      const results = await parseResults('/output');

      expect(results.passed).toBe(5);
      expect(results.failed).toBe(2);
      expect(results.errors).toBe(1);
      expect(results.total).toBe(8);
      expect(results.totalCost).toBe(0.002);
      expect(results.success).toBe(false);
    });

    it('should mark as success when no failures or errors', async () => {
      mockedFs.existsSync.mockImplementation((path: fs.PathLike) =>
        path.toString().includes('index.json')
      );
      mockedFs.readFileSync.mockReturnValue(
        JSON.stringify({
          successful: 5,
          errors: 0,
          total_runs: 5,
          total_cost: 0.001,
        })
      );

      const results = await parseResults('/output');

      expect(results.success).toBe(true);
    });

    it('should parse results.json with summary', async () => {
      mockedFs.existsSync.mockImplementation((path: fs.PathLike) =>
        path.toString().includes('results.json')
      );
      mockedFs.readFileSync.mockReturnValue(
        JSON.stringify({
          summary: {
            passed: 3,
            failed: 1,
            errors: 0,
            total: 4,
            total_cost: 0.001,
          },
        })
      );

      const results = await parseResults('/output');

      expect(results.passed).toBe(3);
      expect(results.failed).toBe(1);
      expect(results.total).toBe(4);
    });

    it('should parse results.json with individual results array', async () => {
      mockedFs.existsSync.mockImplementation((path: fs.PathLike) =>
        path.toString().includes('results.json')
      );
      mockedFs.readFileSync.mockReturnValue(
        JSON.stringify({
          results: [
            { status: 'passed', cost: 0.001 },
            { status: 'passed', cost: 0.001 },
            { status: 'failed', cost: 0.001 },
            { status: 'error', cost: 0 },
          ],
        })
      );

      const results = await parseResults('/output');

      expect(results.passed).toBe(2);
      expect(results.failed).toBe(1);
      expect(results.errors).toBe(1);
      expect(results.total).toBe(4);
      expect(results.totalCost).toBe(0.003);
    });

    it('should return empty results when no files found', async () => {
      mockedFs.existsSync.mockReturnValue(false);

      const results = await parseResults('/output');

      expect(results.passed).toBe(0);
      expect(results.failed).toBe(0);
      expect(results.total).toBe(0);
      expect(results.success).toBe(false);
    });

    it('should handle JSON parse errors gracefully', async () => {
      mockedFs.existsSync.mockReturnValue(true);
      mockedFs.readFileSync.mockReturnValue('invalid json');

      const results = await parseResults('/output');

      expect(mockedCore.warning).toHaveBeenCalled();
      expect(results.total).toBe(0);
    });
  });

  describe('setOutputs', () => {
    const results: TestResults = {
      passed: 5,
      failed: 1,
      errors: 2,
      total: 8,
      totalCost: 0.002345,
      success: false,
    };

    it('should set all outputs', () => {
      mockedFs.existsSync.mockReturnValue(true);

      setOutputs(results, '/output/junit.xml', '/output/report.html');

      expect(mockedCore.setOutput).toHaveBeenCalledWith('passed', '5');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('failed', '1');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('errors', '2');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('total', '8');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('total-cost', '0.002345');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('success', 'false');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('junit-path', '/output/junit.xml');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('html-path', '/output/report.html');
    });

    it('should not set file paths if they do not exist', () => {
      mockedFs.existsSync.mockReturnValue(false);

      setOutputs(results, '/output/junit.xml', '/output/report.html');

      expect(mockedCore.setOutput).not.toHaveBeenCalledWith('junit-path', expect.any(String));
      expect(mockedCore.setOutput).not.toHaveBeenCalledWith('html-path', expect.any(String));
    });
  });

  describe('logSummary', () => {
    it('should log all result information', () => {
      const results: TestResults = {
        passed: 5,
        failed: 1,
        errors: 2,
        total: 8,
        totalCost: 0.002345,
        success: false,
      };

      logSummary(results);

      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('PromptArena Test Results'));
      expect(mockedCore.info).toHaveBeenCalledWith('Total:  8');
      expect(mockedCore.info).toHaveBeenCalledWith('Passed: 5');
      expect(mockedCore.info).toHaveBeenCalledWith('Failed: 1');
      expect(mockedCore.info).toHaveBeenCalledWith('Errors: 2');
      expect(mockedCore.info).toHaveBeenCalledWith('Status: FAILURE');
    });

    it('should show SUCCESS status when successful', () => {
      const results: TestResults = {
        passed: 5,
        failed: 0,
        errors: 0,
        total: 5,
        totalCost: 0.001,
        success: true,
      };

      logSummary(results);

      expect(mockedCore.info).toHaveBeenCalledWith('Status: SUCCESS');
    });
  });
});
