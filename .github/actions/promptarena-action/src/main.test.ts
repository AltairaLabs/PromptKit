import * as core from '@actions/core';

// Mock all dependencies before imports
jest.mock('@actions/core');
jest.mock('./installer');
jest.mock('./runner');
jest.mock('./outputs');

import { installPromptArena } from './installer';
import { runPromptArena, getOutputPaths } from './runner';
import { parseResults, setOutputs, logSummary } from './outputs';

const mockedCore = jest.mocked(core);
const mockedInstallPromptArena = jest.mocked(installPromptArena);
const mockedRunPromptArena = jest.mocked(runPromptArena);
const mockedGetOutputPaths = jest.mocked(getOutputPaths);
const mockedParseResults = jest.mocked(parseResults);
const mockedSetOutputs = jest.mocked(setOutputs);
const mockedLogSummary = jest.mocked(logSummary);

describe('main', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    setupDefaultMocks();
  });

  function setupDefaultMocks(): void {
    // Default input mocks
    mockedCore.getInput.mockImplementation((name: string) => {
      const inputs: Record<string, string> = {
        'config-file': 'config.arena.yaml',
        version: 'latest',
        scenarios: '',
        providers: '',
        regions: '',
        'output-dir': 'out',
        'junit-output': '',
        'fail-on-error': 'true',
        'working-directory': '.',
      };
      return inputs[name] || '';
    });

    // Default mock implementations
    mockedInstallPromptArena.mockResolvedValue('/tools/promptarena');
    mockedRunPromptArena.mockResolvedValue({
      exitCode: 0,
      stdout: 'Tests completed',
      stderr: '',
    });
    mockedGetOutputPaths.mockReturnValue({
      junitPath: './out/junit.xml',
      htmlPath: './out/report.html',
      jsonPath: './out/index.json',
    });
    mockedParseResults.mockResolvedValue({
      passed: 5,
      failed: 0,
      errors: 0,
      total: 5,
      totalCost: 0.001,
      success: true,
    });
    mockedSetOutputs.mockReturnValue();
    mockedLogSummary.mockReturnValue();
  }

  async function reimportAndRun(): Promise<void> {
    // Clear module cache and re-import to trigger run()
    jest.resetModules();

    // Re-mock modules after reset
    jest.doMock('@actions/core', () => mockedCore);
    jest.doMock('./installer', () => ({
      installPromptArena: mockedInstallPromptArena,
    }));
    jest.doMock('./runner', () => ({
      runPromptArena: mockedRunPromptArena,
      getOutputPaths: mockedGetOutputPaths,
    }));
    jest.doMock('./outputs', () => ({
      parseResults: mockedParseResults,
      setOutputs: mockedSetOutputs,
      logSummary: mockedLogSummary,
    }));

    await import('./main');
    // Wait for async operations
    await new Promise((resolve) => setTimeout(resolve, 50));
  }

  describe('basic execution', () => {
    it('should run successfully with minimal inputs', async () => {
      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('PromptArena Action starting...');
      expect(mockedInstallPromptArena).toHaveBeenCalledWith('latest');
    });

    it('should log config file and working directory', async () => {
      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('Config file: config.arena.yaml');
      expect(mockedCore.info).toHaveBeenCalledWith('Working directory: .');
    });

    it('should log version', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'version') return 'v1.2.3';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('Version: v1.2.3');
    });
  });

  describe('installation', () => {
    it('should install promptarena', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'version') return 'v2.0.0';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Installing PromptArena');
      expect(mockedInstallPromptArena).toHaveBeenCalledWith('v2.0.0');
      expect(mockedCore.info).toHaveBeenCalledWith('Installed at: /tools/promptarena');
      expect(mockedCore.endGroup).toHaveBeenCalled();
    });

    it('should default version to latest', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'version') return '';
        return '';
      });

      await reimportAndRun();

      expect(mockedInstallPromptArena).toHaveBeenCalledWith('latest');
    });
  });

  describe('test execution', () => {
    it('should run promptarena with correct inputs', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'my-config.yaml';
        if (name === 'scenarios') return 'test-scenario';
        if (name === 'providers') return 'openai,anthropic';
        if (name === 'regions') return 'us-east-1';
        if (name === 'output-dir') return 'results';
        if (name === 'junit-output') return 'results/junit.xml';
        if (name === 'working-directory') return '/project';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Running PromptArena Tests');
      expect(mockedRunPromptArena).toHaveBeenCalledWith({
        configFile: 'my-config.yaml',
        scenarios: 'test-scenario',
        providers: 'openai,anthropic',
        regions: 'us-east-1',
        outputDir: 'results',
        junitOutput: 'results/junit.xml',
        workingDirectory: '/project',
      });
    });

    it('should handle empty optional inputs', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        return '';
      });

      await reimportAndRun();

      expect(mockedRunPromptArena).toHaveBeenCalledWith({
        configFile: 'config.yaml',
        scenarios: undefined,
        providers: undefined,
        regions: undefined,
        outputDir: 'out',
        junitOutput: undefined,
        workingDirectory: '.',
      });
    });
  });

  describe('result processing', () => {
    it('should parse and log results', async () => {
      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Processing Results');
      expect(mockedParseResults).toHaveBeenCalledWith('out');
      expect(mockedLogSummary).toHaveBeenCalled();
    });

    it('should set outputs', async () => {
      await reimportAndRun();

      expect(mockedSetOutputs).toHaveBeenCalled();
    });

    it('should use correct output paths', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'output-dir') return 'custom-output';
        if (name === 'working-directory') return '/my-project';
        return '';
      });

      await reimportAndRun();

      expect(mockedGetOutputPaths).toHaveBeenCalledWith('/my-project', 'custom-output', '');
    });
  });

  describe('success handling', () => {
    it('should log success when all tests pass', async () => {
      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('All PromptArena tests passed!');
    });

    it('should not call setFailed on success', async () => {
      await reimportAndRun();

      expect(mockedCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe('failure handling', () => {
    it('should call setFailed when tests fail and fail-on-error is true', async () => {
      mockedParseResults.mockResolvedValue({
        passed: 3,
        failed: 2,
        errors: 1,
        total: 6,
        totalCost: 0.002,
        success: false,
      });

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith(
        'PromptArena tests failed: 2 failures, 1 errors'
      );
    });

    it('should call warning when tests fail and fail-on-error is false', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'fail-on-error') return 'false';
        return '';
      });
      mockedParseResults.mockResolvedValue({
        passed: 3,
        failed: 2,
        errors: 0,
        total: 5,
        totalCost: 0.002,
        success: false,
      });

      await reimportAndRun();

      expect(mockedCore.warning).toHaveBeenCalledWith(
        'PromptArena tests failed: 2 failures, 0 errors'
      );
      expect(mockedCore.setFailed).not.toHaveBeenCalled();
    });

    it('should fail when runner exit code is non-zero', async () => {
      mockedRunPromptArena.mockResolvedValue({
        exitCode: 1,
        stdout: '',
        stderr: 'Test error',
      });

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalled();
    });
  });

  describe('error handling', () => {
    it('should handle Error instances', async () => {
      mockedInstallPromptArena.mockRejectedValue(new Error('Download failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Download failed');
    });

    it('should handle non-Error exceptions', async () => {
      mockedInstallPromptArena.mockRejectedValue('String error');

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('An unexpected error occurred');
    });

    it('should handle runner errors', async () => {
      mockedRunPromptArena.mockRejectedValue(new Error('Runner failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Runner failed');
    });

    it('should handle parse errors', async () => {
      mockedParseResults.mockRejectedValue(new Error('Parse failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Parse failed');
    });
  });
});
