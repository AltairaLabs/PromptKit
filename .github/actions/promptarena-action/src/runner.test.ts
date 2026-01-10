import * as exec from '@actions/exec';
import { runPromptArena, getOutputPaths, RunnerInputs } from './runner';

const mockedExec = jest.mocked(exec);

describe('runner', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('runPromptArena', () => {
    const baseInputs: RunnerInputs = {
      configFile: 'config.arena.yaml',
      outputDir: 'out',
      workingDirectory: '/workspace',
    };

    it('should run promptarena with basic arguments', async () => {
      mockedExec.exec.mockResolvedValue(0);

      const result = await runPromptArena(baseInputs);

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'promptarena',
        expect.arrayContaining([
          'run',
          '--config', 'config.arena.yaml',
          '--ci',
          '--format', 'json,junit',
          '--out', 'out',
        ]),
        expect.objectContaining({
          cwd: '/workspace',
        })
      );
      expect(result.exitCode).toBe(0);
    });

    it('should include scenarios filter', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await runPromptArena({
        ...baseInputs,
        scenarios: 'scenario1, scenario2',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'promptarena',
        expect.arrayContaining(['--scenario', 'scenario1', '--scenario', 'scenario2']),
        expect.any(Object)
      );
    });

    it('should include providers filter', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await runPromptArena({
        ...baseInputs,
        providers: 'openai,anthropic',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'promptarena',
        expect.arrayContaining(['--provider', 'openai', '--provider', 'anthropic']),
        expect.any(Object)
      );
    });

    it('should include regions filter', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await runPromptArena({
        ...baseInputs,
        regions: 'us-east-1,eu-west-1',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'promptarena',
        expect.arrayContaining(['--region', 'us-east-1', '--region', 'eu-west-1']),
        expect.any(Object)
      );
    });

    it('should include junit output path when specified', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await runPromptArena({
        ...baseInputs,
        junitOutput: '/custom/junit.xml',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'promptarena',
        expect.arrayContaining(['--junit-file', '/custom/junit.xml']),
        expect.any(Object)
      );
    });

    it('should capture stdout and stderr', async () => {
      mockedExec.exec.mockImplementation(async (_cmd, _args, options) => {
        options?.listeners?.stdout?.(Buffer.from('test output'));
        options?.listeners?.stderr?.(Buffer.from('test error'));
        return 0;
      });

      const result = await runPromptArena(baseInputs);

      expect(result.stdout).toBe('test output');
      expect(result.stderr).toBe('test error');
    });

    it('should return non-zero exit code on failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      const result = await runPromptArena(baseInputs);

      expect(result.exitCode).toBe(1);
    });
  });

  describe('getOutputPaths', () => {
    it('should return default paths', () => {
      const paths = getOutputPaths('/workspace', 'out');

      expect(paths.junitPath).toBe('/workspace/out/junit.xml');
      expect(paths.htmlPath).toBe('/workspace/out/report.html');
      expect(paths.jsonPath).toBe('/workspace/out/results.json');
    });

    it('should use custom junit output path when specified', () => {
      const paths = getOutputPaths('/workspace', 'out', '/custom/junit.xml');

      expect(paths.junitPath).toBe('/custom/junit.xml');
      expect(paths.htmlPath).toBe('/workspace/out/report.html');
    });
  });
});
