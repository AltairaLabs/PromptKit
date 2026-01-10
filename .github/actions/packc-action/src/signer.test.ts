import * as exec from '@actions/exec';
import * as fs from 'fs';
import { sign, verify } from './signer';

jest.mock('fs');

const mockedExec = jest.mocked(exec);
const mockedFs = jest.mocked(fs);

describe('signer', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedFs.existsSync.mockReturnValue(false);
  });

  describe('sign', () => {
    it('should sign with key file path', async () => {
      mockedExec.exec.mockResolvedValue(0);

      const result = await sign({
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123',
        cosignKey: '/path/to/key.pem',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'cosign',
        ['sign', '--key', '/path/to/key.pem', 'ghcr.io/test/pack@sha256:abc123', '--yes'],
        expect.any(Object)
      );
      expect(result.signature).toBeDefined();
    });

    it('should write key content to temp file when key content provided', async () => {
      mockedExec.exec.mockResolvedValue(0);
      mockedFs.existsSync.mockReturnValue(true);

      await sign({
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123',
        cosignKey: '-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----',
      });

      expect(mockedFs.writeFileSync).toHaveBeenCalledWith(
        expect.stringContaining('cosign-key-'),
        expect.stringContaining('PRIVATE KEY'),
        { mode: 0o600 }
      );
      expect(mockedFs.unlinkSync).toHaveBeenCalled();
    });

    it('should pass cosign password via environment', async () => {
      let capturedEnv: Record<string, string> | undefined;
      mockedExec.exec.mockImplementation(async (_cmd, _args, options) => {
        capturedEnv = options?.env;
        return 0;
      });

      await sign({
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123',
        cosignKey: '/path/to/key.pem',
        cosignPassword: 'secret',
      });

      expect(capturedEnv?.COSIGN_PASSWORD).toBe('secret');
    });

    it('should throw on signing failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      await expect(
        sign({
          registryUrl: 'ghcr.io/test/pack:1.0.0',
          digest: 'sha256:abc123',
          cosignKey: '/path/to/key.pem',
        })
      ).rejects.toThrow('Cosign signing failed');
    });

    it('should use registryUrl directly when no digest provided', async () => {
      mockedExec.exec.mockResolvedValue(0);

      await sign({
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: '',
        cosignKey: '/path/to/key.pem',
      });

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'cosign',
        expect.arrayContaining(['ghcr.io/test/pack:1.0.0']),
        expect.any(Object)
      );
    });

    it('should construct signature reference from imageRef with digest', async () => {
      mockedExec.exec.mockResolvedValue(0);

      const result = await sign({
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc12345',
        cosignKey: '/path/to/key.pem',
      });

      // Constructs the .sig reference from the imageRef when no signature digest in output
      expect(result.signature).toBe('ghcr.io/test/pack:sha256-abc123def456abc123def456abc123def456abc123def456abc123def456abc12345.sig');
    });
  });

  describe('verify', () => {
    it('should verify with public key path', async () => {
      mockedExec.exec.mockResolvedValue(0);

      const result = await verify('ghcr.io/test/pack:1.0.0', '/path/to/key.pub');

      expect(mockedExec.exec).toHaveBeenCalledWith(
        'cosign',
        ['verify', '--key', '/path/to/key.pub', 'ghcr.io/test/pack:1.0.0'],
        expect.any(Object)
      );
      expect(result).toBe(true);
    });

    it('should write public key content to temp file when content provided', async () => {
      mockedExec.exec.mockResolvedValue(0);
      mockedFs.existsSync.mockReturnValue(true);

      await verify(
        'ghcr.io/test/pack:1.0.0',
        '-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----'
      );

      expect(mockedFs.writeFileSync).toHaveBeenCalledWith(
        expect.stringContaining('cosign-pub-'),
        expect.stringContaining('PUBLIC KEY'),
        { mode: 0o644 }
      );
      expect(mockedFs.unlinkSync).toHaveBeenCalled();
    });

    it('should return false on verification failure', async () => {
      mockedExec.exec.mockResolvedValue(1);

      const result = await verify('ghcr.io/test/pack:1.0.0', '/path/to/key.pub');

      expect(result).toBe(false);
    });
  });
});
