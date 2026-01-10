import * as core from '@actions/core';
import * as exec from '@actions/exec';
import * as fs from 'node:fs';
import { compile, validate, parsePackFile } from './compiler';

jest.mock('@actions/core');
jest.mock('@actions/exec');
jest.mock('node:fs');

const mockedCore = jest.mocked(core);
const mockedExec = jest.mocked(exec);
const mockedFs = jest.mocked(fs);

describe('compiler', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('compile', () => {
    beforeEach(() => {
      mockedExec.exec.mockImplementation(
        async (
          _cmd: string,
          _args?: string[],
          options?: exec.ExecOptions
        ): Promise<number> => {
          if (options?.listeners?.stdout) {
            options.listeners.stdout(
              Buffer.from('Compiling 3 prompts into pack \'test-pack\'...\n')
            );
            options.listeners.stdout(
              Buffer.from('Pack compiled successfully: test-pack.pack.json\n')
            );
          }
          return 0;
        }
      );
    });

    it('should compile pack with basic inputs', async () => {
      const result = await compile({
        configFile: 'config.yaml',
        workingDirectory: '/project',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'packc',
        ['compile', '--config', 'config.yaml'],
        expect.objectContaining({ cwd: '/project' })
      );
      expect(result.exitCode).toBe(0);
      expect(result.prompts).toBe(3);
      expect(result.packId).toBe('test-pack');
    });

    it('should include pack-id flag when provided', async () => {
      await compile({
        configFile: 'config.yaml',
        packId: 'my-pack',
        workingDirectory: '.',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'packc',
        ['compile', '--config', 'config.yaml', '--id', 'my-pack'],
        expect.any(Object)
      );
    });

    it('should include output flag when provided', async () => {
      await compile({
        configFile: 'config.yaml',
        output: './dist/output.pack.json',
        workingDirectory: '.',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'packc',
        ['compile', '--config', 'config.yaml', '--output', './dist/output.pack.json'],
        expect.any(Object)
      );
    });

    it('should throw on non-zero exit code', async () => {
      mockedExec.exec.mockResolvedValue(1);

      await expect(
        compile({
          configFile: 'config.yaml',
          workingDirectory: '.',
        })
      ).rejects.toThrow('packc compile failed with exit code 1');
    });

    it('should log stdout and stderr', async () => {
      mockedExec.exec.mockImplementation(
        async (
          _cmd: string,
          _args?: string[],
          options?: exec.ExecOptions
        ): Promise<number> => {
          if (options?.listeners?.stdout) {
            options.listeners.stdout(Buffer.from('stdout output'));
          }
          if (options?.listeners?.stderr) {
            options.listeners.stderr(Buffer.from('stderr output'));
          }
          return 0;
        }
      );

      await compile({
        configFile: 'config.yaml',
        workingDirectory: '.',
      });

      expect(mockedCore.info).toHaveBeenCalledWith('--- stdout ---');
      expect(mockedCore.info).toHaveBeenCalledWith('stdout output');
      expect(mockedCore.info).toHaveBeenCalledWith('--- stderr ---');
      expect(mockedCore.info).toHaveBeenCalledWith('stderr output');
    });

    it('should parse tools count from output', async () => {
      mockedExec.exec.mockImplementation(
        async (
          _cmd: string,
          _args?: string[],
          options?: exec.ExecOptions
        ): Promise<number> => {
          if (options?.listeners?.stdout) {
            options.listeners.stdout(
              Buffer.from('Including 2 tool definitions in pack\n')
            );
            options.listeners.stdout(
              Buffer.from('Pack compiled successfully: test.pack.json\n')
            );
          }
          return 0;
        }
      );

      const result = await compile({
        configFile: 'config.yaml',
        workingDirectory: '.',
      });

      expect(result.tools).toBe(2);
    });

    it('should derive pack file from pack-id when not in output', async () => {
      mockedExec.exec.mockImplementation(async (): Promise<number> => 0);

      const result = await compile({
        configFile: 'config.yaml',
        packId: 'my-pack',
        workingDirectory: '/project',
      });

      expect(result.packFile).toBe('/project/my-pack.pack.json');
    });

    it('should use output path when provided', async () => {
      mockedExec.exec.mockImplementation(async (): Promise<number> => 0);

      const result = await compile({
        configFile: 'config.yaml',
        output: '/absolute/path/output.pack.json',
        workingDirectory: '/project',
      });

      expect(result.packFile).toBe('/absolute/path/output.pack.json');
    });

    it('should parse Contains X prompts format', async () => {
      mockedExec.exec.mockImplementation(
        async (
          _cmd: string,
          _args?: string[],
          options?: exec.ExecOptions
        ): Promise<number> => {
          if (options?.listeners?.stdout) {
            options.listeners.stdout(Buffer.from('Contains 5 prompts\n'));
            options.listeners.stdout(Buffer.from('Contains 3 tools\n'));
          }
          return 0;
        }
      );

      const result = await compile({
        configFile: 'config.yaml',
        workingDirectory: '.',
      });

      expect(result.prompts).toBe(5);
      expect(result.tools).toBe(3);
    });
  });

  describe('validate', () => {
    beforeEach(() => {
      mockedFs.existsSync.mockReturnValue(true);
    });

    it('should validate pack successfully', async () => {
      mockedExec.exec.mockResolvedValue(0);

      const result = await validate('/path/to/pack.json');

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'packc',
        ['validate', '/path/to/pack.json'],
        expect.any(Object)
      );
      expect(result).toBe(true);
      expect(mockedCore.info).toHaveBeenCalledWith('Pack validation passed');
    });

    it('should return false on validation failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      const result = await validate('/path/to/pack.json');

      expect(result).toBe(false);
      expect(mockedCore.warning).toHaveBeenCalledWith(
        'Pack validation failed with exit code 1'
      );
    });

    it('should throw when pack file does not exist', async () => {
      mockedFs.existsSync.mockReturnValue(false);

      await expect(validate('/path/to/nonexistent.json')).rejects.toThrow(
        'Pack file not found: /path/to/nonexistent.json'
      );
    });

    it('should log stdout from validation', async () => {
      mockedExec.exec.mockImplementation(
        async (
          _cmd: string,
          _args?: string[],
          options?: exec.ExecOptions
        ): Promise<number> => {
          if (options?.listeners?.stdout) {
            options.listeners.stdout(Buffer.from('Validation output'));
          }
          return 0;
        }
      );

      await validate('/path/to/pack.json');

      expect(mockedCore.info).toHaveBeenCalledWith('Validation output');
    });

    it('should log stderr from validation', async () => {
      mockedExec.exec.mockImplementation(
        async (
          _cmd: string,
          _args?: string[],
          options?: exec.ExecOptions
        ): Promise<number> => {
          if (options?.listeners?.stderr) {
            options.listeners.stderr(Buffer.from('Validation warning'));
          }
          return 0;
        }
      );

      await validate('/path/to/pack.json');

      expect(mockedCore.info).toHaveBeenCalledWith('Validation warning');
    });
  });

  describe('parsePackFile', () => {
    it('should return zeros when file does not exist', () => {
      mockedFs.existsSync.mockReturnValue(false);

      const result = parsePackFile('/path/to/nonexistent.pack.json');

      expect(result).toEqual({ prompts: 0, tools: 0, packId: '' });
    });

    it('should parse a valid pack file', () => {
      mockedFs.existsSync.mockReturnValue(true);
      mockedFs.readFileSync.mockReturnValue(
        JSON.stringify({
          id: 'test-pack',
          prompts: {
            prompt1: {},
            prompt2: {},
            prompt3: {},
          },
          tools: {
            tool1: {},
            tool2: {},
          },
        })
      );

      const result = parsePackFile('/path/to/test.pack.json');

      expect(result).toEqual({
        packId: 'test-pack',
        prompts: 3,
        tools: 2,
      });
    });

    it('should handle pack file with no prompts or tools', () => {
      mockedFs.existsSync.mockReturnValue(true);
      mockedFs.readFileSync.mockReturnValue(
        JSON.stringify({
          id: 'empty-pack',
        })
      );

      const result = parsePackFile('/path/to/empty.pack.json');

      expect(result).toEqual({
        packId: 'empty-pack',
        prompts: 0,
        tools: 0,
      });
    });

    it('should return zeros on JSON parse error', () => {
      mockedFs.existsSync.mockReturnValue(true);
      mockedFs.readFileSync.mockReturnValue('invalid json');

      const result = parsePackFile('/path/to/invalid.pack.json');

      expect(result).toEqual({ prompts: 0, tools: 0, packId: '' });
    });

    it('should return zeros when readFileSync throws', () => {
      mockedFs.existsSync.mockReturnValue(true);
      mockedFs.readFileSync.mockImplementation(() => {
        throw new Error('Read error');
      });

      const result = parsePackFile('/path/to/error.pack.json');

      expect(result).toEqual({ prompts: 0, tools: 0, packId: '' });
    });
  });
});
