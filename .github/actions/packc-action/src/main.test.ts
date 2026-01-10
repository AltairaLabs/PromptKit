import * as core from '@actions/core';

// Mock all dependencies before imports
jest.mock('@actions/core');
jest.mock('./installer');
jest.mock('./compiler');
jest.mock('./publisher');
jest.mock('./signer');
jest.mock('./outputs');

import { installPackC, installORAS, installCosign } from './installer';
import { compile, validate, parsePackFile } from './compiler';
import { publish, logout } from './publisher';
import { sign } from './signer';
import { setOutputs, logSummary } from './outputs';

const mockedCore = jest.mocked(core);
const mockedInstallPackC = jest.mocked(installPackC);
const mockedInstallORAS = jest.mocked(installORAS);
const mockedInstallCosign = jest.mocked(installCosign);
const mockedCompile = jest.mocked(compile);
const mockedValidate = jest.mocked(validate);
const mockedParsePackFile = jest.mocked(parsePackFile);
const mockedPublish = jest.mocked(publish);
const mockedLogout = jest.mocked(logout);
const mockedSign = jest.mocked(sign);
const mockedSetOutputs = jest.mocked(setOutputs);
const mockedLogSummary = jest.mocked(logSummary);

// Store the run function from main module
let runMain: () => Promise<void>;

describe('main', () => {
  beforeAll(async () => {
    // Import main module once to get the run function behavior
    // The module auto-runs, so we need to mock everything first
    setupDefaultMocks();
  });

  beforeEach(() => {
    jest.clearAllMocks();
    setupDefaultMocks();
  });

  function setupDefaultMocks(): void {
    // Default input mocks
    mockedCore.getInput.mockImplementation((name: string) => {
      const inputs: Record<string, string> = {
        'config-file': 'config.yaml',
        'pack-id': 'test-pack',
        version: '1.0.0',
        'packc-version': 'latest',
        output: '',
        validate: 'true',
        registry: '',
        repository: '',
        username: '',
        password: '',
        sign: 'false',
        'cosign-key': '',
        'cosign-password': '',
        'working-directory': '.',
      };
      return inputs[name] || '';
    });

    // Default mock implementations
    mockedInstallPackC.mockResolvedValue('/tools/packc');
    mockedInstallORAS.mockResolvedValue('/tools/oras');
    mockedInstallCosign.mockResolvedValue('/tools/cosign');
    mockedCompile.mockResolvedValue({
      packFile: '/output/test.pack.json',
      packId: 'test-pack',
      prompts: 2,
      tools: 1,
      exitCode: 0,
    });
    mockedValidate.mockResolvedValue(true);
    mockedParsePackFile.mockReturnValue({
      packId: 'test-pack',
      prompts: 2,
      tools: 1,
    });
    mockedPublish.mockResolvedValue({
      registryUrl: 'ghcr.io/test/pack:1.0.0',
      digest: 'sha256:abc123',
      tags: ['1.0.0', 'latest'],
    });
    mockedSign.mockResolvedValue({
      signature: 'sig123',
    });
    mockedLogout.mockResolvedValue();
    mockedSetOutputs.mockReturnValue();
    mockedLogSummary.mockReturnValue();
  }

  async function reimportAndRun(): Promise<void> {
    // Clear module cache and re-import to trigger run()
    jest.resetModules();

    // Re-mock modules after reset
    jest.doMock('@actions/core', () => mockedCore);
    jest.doMock('./installer', () => ({
      installPackC: mockedInstallPackC,
      installORAS: mockedInstallORAS,
      installCosign: mockedInstallCosign,
    }));
    jest.doMock('./compiler', () => ({
      compile: mockedCompile,
      validate: mockedValidate,
      parsePackFile: mockedParsePackFile,
    }));
    jest.doMock('./publisher', () => ({
      publish: mockedPublish,
      logout: mockedLogout,
    }));
    jest.doMock('./signer', () => ({
      sign: mockedSign,
    }));
    jest.doMock('./outputs', () => ({
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

      expect(mockedCore.info).toHaveBeenCalledWith('PackC Action starting...');
      expect(mockedInstallPackC).toHaveBeenCalledWith('latest');
    });

    it('should log action start with config file and working directory', async () => {
      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('Config file: config.yaml');
      expect(mockedCore.info).toHaveBeenCalledWith('Working directory: .');
    });

    it('should log pack ID when provided', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'pack-id') return 'my-pack';
        if (name === 'working-directory') return '.';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('Pack ID: my-pack');
    });
  });

  describe('tool installation', () => {
    it('should install packc tool', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'packc-version') return 'v1.2.3';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Installing PackC');
      expect(mockedInstallPackC).toHaveBeenCalledWith('v1.2.3');
      expect(mockedCore.endGroup).toHaveBeenCalled();
    });

    it('should install ORAS when registry is configured', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'test/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Installing ORAS');
      expect(mockedInstallORAS).toHaveBeenCalled();
    });

    it('should not install ORAS when registry is not configured', async () => {
      await reimportAndRun();

      expect(mockedInstallORAS).not.toHaveBeenCalled();
    });

    it('should install Cosign when signing is enabled', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'sign') return 'true';
        if (name === 'cosign-key') return 'key.pem';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Installing Cosign');
      expect(mockedInstallCosign).toHaveBeenCalled();
    });

    it('should not install Cosign when signing is disabled', async () => {
      await reimportAndRun();

      expect(mockedInstallCosign).not.toHaveBeenCalled();
    });
  });

  describe('pack compilation', () => {
    it('should compile pack with correct inputs', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'my-config.yaml';
        if (name === 'pack-id') return 'my-pack';
        if (name === 'output') return './dist';
        if (name === 'working-directory') return '/project';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Compiling Pack');
      expect(mockedCompile).toHaveBeenCalledWith({
        configFile: 'my-config.yaml',
        packId: 'my-pack',
        output: './dist',
        workingDirectory: '/project',
      });
    });

    it('should validate pack by default', async () => {
      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Validating Pack');
      expect(mockedValidate).toHaveBeenCalledWith('/output/test.pack.json');
    });

    it('should skip validation when disabled', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'validate') return 'false';
        return '';
      });

      await reimportAndRun();

      expect(mockedValidate).not.toHaveBeenCalled();
    });

    it('should warn when validation has warnings', async () => {
      mockedValidate.mockResolvedValue(false);

      await reimportAndRun();

      expect(mockedCore.warning).toHaveBeenCalledWith('Pack validation had warnings');
    });

    it('should parse pack file for accurate counts', async () => {
      mockedParsePackFile.mockReturnValue({
        packId: 'parsed-pack',
        prompts: 5,
        tools: 3,
      });

      await reimportAndRun();

      expect(mockedParsePackFile).toHaveBeenCalledWith('/output/test.pack.json');
    });
  });

  describe('pack publishing', () => {
    it('should publish pack when registry is configured', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'version') return '2.0.0';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        if (name === 'username') return 'user';
        if (name === 'password') return 'pass';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Publishing to Registry');
      expect(mockedPublish).toHaveBeenCalledWith({
        packFile: '/output/test.pack.json',
        packId: 'test-pack',
        version: '2.0.0',
        registry: 'ghcr.io',
        repository: 'org/pack',
        username: 'user',
        password: 'pass',
      });
    });

    it('should not publish when registry is not configured', async () => {
      await reimportAndRun();

      expect(mockedPublish).not.toHaveBeenCalled();
    });

    it('should log registry info when configured', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('Registry: ghcr.io/org/pack');
    });
  });

  describe('pack signing', () => {
    it('should sign pack when signing is enabled and pack is published', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'sign') return 'true';
        if (name === 'cosign-key') return '/keys/cosign.key';
        if (name === 'cosign-password') return 'secret';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.startGroup).toHaveBeenCalledWith('Signing Pack');
      expect(mockedSign).toHaveBeenCalledWith({
        registryUrl: 'ghcr.io/test/pack:1.0.0',
        digest: 'sha256:abc123',
        cosignKey: '/keys/cosign.key',
        cosignPassword: 'secret',
      });
    });

    it('should not sign when signing is disabled', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'sign') return 'false';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedSign).not.toHaveBeenCalled();
    });

    it('should not sign when cosign key is not provided', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'sign') return 'true';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedSign).not.toHaveBeenCalled();
    });

    it('should not sign when pack is not published', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'sign') return 'true';
        if (name === 'cosign-key') return '/keys/cosign.key';
        return '';
      });

      await reimportAndRun();

      expect(mockedSign).not.toHaveBeenCalled();
    });
  });

  describe('outputs and cleanup', () => {
    it('should set outputs after completion', async () => {
      await reimportAndRun();

      expect(mockedSetOutputs).toHaveBeenCalled();
    });

    it('should log summary after completion', async () => {
      await reimportAndRun();

      expect(mockedLogSummary).toHaveBeenCalled();
    });

    it('should logout from registry after publishing', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        if (name === 'username') return 'user';
        return '';
      });

      await reimportAndRun();

      expect(mockedLogout).toHaveBeenCalledWith('ghcr.io');
    });

    it('should not logout when username is not provided', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedLogout).not.toHaveBeenCalled();
    });

    it('should log success message on completion', async () => {
      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('PackC Action completed successfully!');
    });
  });

  describe('error handling', () => {
    it('should handle Error instances', async () => {
      mockedInstallPackC.mockRejectedValue(new Error('Download failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Download failed');
    });

    it('should handle non-Error exceptions', async () => {
      mockedInstallPackC.mockRejectedValue('String error');

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('An unexpected error occurred');
    });

    it('should handle compile errors', async () => {
      mockedCompile.mockRejectedValue(new Error('Compile failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Compile failed');
    });

    it('should handle publish errors', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });
      mockedPublish.mockRejectedValue(new Error('Publish failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Publish failed');
    });

    it('should handle sign errors', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'sign') return 'true';
        if (name === 'cosign-key') return 'key.pem';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });
      mockedSign.mockRejectedValue(new Error('Sign failed'));

      await reimportAndRun();

      expect(mockedCore.setFailed).toHaveBeenCalledWith('Sign failed');
    });
  });

  describe('input parsing', () => {
    it('should default version to latest when empty', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'version') return '';
        if (name === 'registry') return 'ghcr.io';
        if (name === 'repository') return 'org/pack';
        return '';
      });

      await reimportAndRun();

      expect(mockedPublish).toHaveBeenCalledWith(
        expect.objectContaining({ version: 'latest' })
      );
    });

    it('should default packc-version to latest when empty', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'packc-version') return '';
        return '';
      });

      await reimportAndRun();

      expect(mockedInstallPackC).toHaveBeenCalledWith('latest');
    });

    it('should default working-directory to current directory', async () => {
      mockedCore.getInput.mockImplementation((name: string) => {
        if (name === 'config-file') return 'config.yaml';
        if (name === 'working-directory') return '';
        return '';
      });

      await reimportAndRun();

      expect(mockedCore.info).toHaveBeenCalledWith('Working directory: .');
    });
  });
});
