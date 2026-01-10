import * as core from '@actions/core';
import { setOutputs, logSummary } from './outputs';

const mockedCore = jest.mocked(core);

describe('outputs', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('setOutputs', () => {
    it('should set compile outputs', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      setOutputs(compileResult);

      expect(mockedCore.setOutput).toHaveBeenCalledWith('pack-file', '/path/to/test.pack.json');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('pack-id', 'test-pack');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('prompts', '5');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('tools', '2');
    });

    it('should set publish outputs when provided', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      const publishResult = {
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123',
        tags: ['1.0.0', 'latest'],
      };

      setOutputs(compileResult, publishResult);

      expect(mockedCore.setOutput).toHaveBeenCalledWith('registry-url', 'ghcr.io/test/pack:1.0.0');
      expect(mockedCore.setOutput).toHaveBeenCalledWith('digest', 'sha256:abc123');
    });

    it('should set sign outputs when provided', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      const signResult = {
        signature: 'sha256:sig123',
      };

      setOutputs(compileResult, undefined, signResult);

      expect(mockedCore.setOutput).toHaveBeenCalledWith('signature', 'sha256:sig123');
    });

    it('should not set publish outputs when not provided', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      setOutputs(compileResult);

      expect(mockedCore.setOutput).not.toHaveBeenCalledWith('registry-url', expect.any(String));
      expect(mockedCore.setOutput).not.toHaveBeenCalledWith('digest', expect.any(String));
    });
  });

  describe('logSummary', () => {
    it('should log compile summary', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      logSummary(compileResult);

      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('PackC Action Summary'));
      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('test-pack'));
      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('5'));
      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('2'));
    });

    it('should log publish summary when provided', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      const publishResult = {
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123',
        tags: ['1.0.0', 'latest'],
      };

      logSummary(compileResult, publishResult);

      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('Published'));
      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('ghcr.io/test/pack:1.0.0'));
      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('1.0.0, latest'));
    });

    it('should log sign summary when provided', () => {
      const compileResult = {
        packFile: '/path/to/test.pack.json',
        packId: 'test-pack',
        prompts: 5,
        tools: 2,
        exitCode: 0,
      };

      const signResult = {
        signature: 'sha256:sig123',
      };

      logSummary(compileResult, undefined, signResult);

      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('Signed'));
      expect(mockedCore.info).toHaveBeenCalledWith(expect.stringContaining('sha256:sig123'));
    });
  });
});
