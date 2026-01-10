import * as core from '@actions/core';
import * as exec from '@actions/exec';
import { publish, login, logout } from './publisher';

jest.mock('@actions/core');

const mockedCore = jest.mocked(core);
const mockedExec = jest.mocked(exec);

describe('publisher', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('login', () => {
    it('should login successfully', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await login('ghcr.io', 'user', 'token');

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'oras',
        ['login', 'ghcr.io', '-u', 'user', '--password-stdin'],
        expect.objectContaining({
          input: Buffer.from('token'),
        })
      );
    });

    it('should throw on login failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      await expect(login('ghcr.io', 'user', 'token')).rejects.toThrow(
        'Failed to login to registry'
      );
    });

    it('should capture stderr on login failure', async () => {
      mockedExec.exec.mockImplementation(async (_cmd, _args, options) => {
        if (options?.listeners?.stderr) {
          options.listeners.stderr(Buffer.from('authentication failed'));
        }
        return 1;
      });

      await expect(login('ghcr.io', 'user', 'token')).rejects.toThrow(
        'authentication failed'
      );
    });
  });

  describe('logout', () => {
    it('should logout successfully', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await logout('ghcr.io');

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'oras',
        ['logout', 'ghcr.io'],
        expect.objectContaining({ ignoreReturnCode: true })
      );
    });

    it('should not throw on logout failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      await expect(logout('ghcr.io')).resolves.toBeUndefined();
    });
  });

  describe('publish', () => {
    it('should publish without credentials', async () => {
      mockedExec.exec.mockImplementation(async (_cmd, _args, options) => {
        if (options?.listeners) {
          // Simulate ORAS output with digest
          options.listeners.stdout?.(
            Buffer.from('Pushed ghcr.io/test/pack:1.0.0@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      const result = await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: '1.0.0',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(result.registryUrl).toBe('ghcr.io/test/pack:1.0.0');
      expect(result.digest).toBe('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1');
      expect(result.tags).toContain('1.0.0');
    });

    it('should login before publishing when credentials provided', async () => {
      const execCalls: string[] = [];

      mockedExec.exec.mockImplementation(async (cmd, args, options) => {
        execCalls.push(`${cmd} ${(args as string[])[0]}`);
        if (options?.listeners?.stdout) {
          options.listeners.stdout(
            Buffer.from('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: '1.0.0',
        registry: 'ghcr.io',
        repository: 'test/pack',
        username: 'user',
        password: 'token',
      });

      expect(execCalls[0]).toBe('oras login');
      expect(execCalls[1]).toBe('oras push');
    });

    it('should throw on publish failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      await expect(
        publish({
          packFile: 'test.pack.json',
          packId: 'test-pack',
          version: '1.0.0',
          registry: 'ghcr.io',
          repository: 'test/pack',
        })
      ).rejects.toThrow('Failed to publish pack');
    });

    it('should tag as latest for semver versions', async () => {
      const tagCalls: string[][] = [];

      mockedExec.exec.mockImplementation(async (cmd, args, options) => {
        if ((args as string[])[0] === 'tag') {
          tagCalls.push(args as string[]);
        }
        if (options?.listeners?.stdout) {
          options.listeners.stdout(
            Buffer.from('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      const result = await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: '1.2.3',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(tagCalls.length).toBe(1);
      expect(tagCalls[0]).toContain('ghcr.io/test/pack:latest');
      expect(result.tags).toContain('latest');
    });

    it('should not tag as latest when version is already latest', async () => {
      const tagCalls: string[][] = [];

      mockedExec.exec.mockImplementation(async (cmd, args, options) => {
        if ((args as string[])[0] === 'tag') {
          tagCalls.push(args as string[]);
        }
        if (options?.listeners?.stdout) {
          options.listeners.stdout(
            Buffer.from('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: 'latest',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(tagCalls.length).toBe(0);
    });

    it('should log stderr output from publish', async () => {
      mockedExec.exec.mockImplementation(async (_cmd, _args, options) => {
        if (options?.listeners?.stderr) {
          options.listeners.stderr(Buffer.from('warning: some message'));
        }
        if (options?.listeners?.stdout) {
          options.listeners.stdout(
            Buffer.from('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: 'foo',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(mockedCore.info).toHaveBeenCalledWith('--- stderr ---');
      expect(mockedCore.info).toHaveBeenCalledWith('warning: some message');
    });

    it('should warn when tagging as latest fails', async () => {
      mockedExec.exec.mockImplementation(async (_cmd, args, options) => {
        if ((args as string[])[0] === 'tag') {
          return 1; // Tag fails
        }
        if (options?.listeners?.stdout) {
          options.listeners.stdout(
            Buffer.from('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      const result = await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: '1.0.0',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(mockedCore.warning).toHaveBeenCalledWith(
        expect.stringContaining('Failed to tag as latest')
      );
      expect(result.tags).not.toContain('latest');
    });

    it('should parse digest from @sha256 format', async () => {
      mockedExec.exec.mockImplementation(async (_cmd, _args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(
            Buffer.from('Pushed image@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1')
          );
        }
        return 0;
      });

      const result = await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: 'foo',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(result.digest).toBe('sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1');
    });

    it('should return empty digest when not found in output', async () => {
      mockedExec.exec.mockImplementation(async () => 0);

      const result = await publish({
        packFile: 'test.pack.json',
        packId: 'test-pack',
        version: 'foo',
        registry: 'ghcr.io',
        repository: 'test/pack',
      });

      expect(result.digest).toBe('');
    });
  });
});
