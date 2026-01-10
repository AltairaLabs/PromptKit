import * as core from '@actions/core';
import * as tc from '@actions/tool-cache';
import * as fs from 'node:fs';
import * as os from 'node:os';
import { installPackC, installORAS, installCosign } from './installer';

jest.mock('@actions/core');
jest.mock('@actions/tool-cache');
jest.mock('node:fs');
jest.mock('node:os');

const mockedCore = jest.mocked(core);
const mockedTc = jest.mocked(tc);
const mockedFs = jest.mocked(fs);
const mockedOs = jest.mocked(os);

// Mock global fetch
const mockFetch = jest.fn();
global.fetch = mockFetch;

describe('installer', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    // Default to Linux x64
    mockedOs.platform.mockReturnValue('linux');
    mockedOs.arch.mockReturnValue('x64');
    mockedOs.tmpdir.mockReturnValue('/tmp');
    mockedFs.existsSync.mockReturnValue(true);
  });

  describe('installPackC', () => {
    it('should install packc with specific version', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/packc/v1.0.0');

      const result = await installPackC('v1.0.0');

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('PromptKit_1.0.0_Linux_x86_64.tar.gz')
      );
      expect(mockedTc.cacheDir).toHaveBeenCalled();
      expect(mockedCore.addPath).toHaveBeenCalledWith('/tools/packc/v1.0.0');
      expect(result).toBe('/tools/packc/v1.0.0/packc');
    });

    it('should use cached version if available', async () => {
      mockedTc.find.mockReturnValue('/tools/packc/v1.0.0');

      const result = await installPackC('v1.0.0');

      expect(mockedTc.downloadTool).not.toHaveBeenCalled();
      expect(mockedCore.info).toHaveBeenCalledWith('Found cached packc v1.0.0');
      expect(result).toBe('/tools/packc/v1.0.0/packc');
    });

    it('should resolve latest version', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ tag_name: 'v2.0.0' }),
      });
      mockedTc.find.mockReturnValue('/tools/packc/v2.0.0');

      await installPackC('latest');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('AltairaLabs/PromptKit/releases/latest'),
        expect.any(Object)
      );
      expect(mockedCore.info).toHaveBeenCalledWith('Resolving latest PackC version...');
    });

    it('should add v prefix if missing', async () => {
      mockedTc.find.mockReturnValue('/tools/packc/v1.0.0');

      await installPackC('1.0.0');

      expect(mockedTc.find).toHaveBeenCalledWith('packc', 'v1.0.0', 'x86_64');
    });

    it('should throw if binary not found after extraction', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedFs.existsSync.mockReturnValue(false);

      await expect(installPackC('v1.0.0')).rejects.toThrow('packc binary not found');
    });

    it('should handle Windows platform', async () => {
      mockedOs.platform.mockReturnValue('win32');
      mockedTc.find.mockReturnValue('/tools/packc/v1.0.0');

      const result = await installPackC('v1.0.0');

      expect(result).toBe('/tools/packc/v1.0.0/packc.exe');
    });

    it('should handle Darwin platform', async () => {
      mockedOs.platform.mockReturnValue('darwin');
      mockedTc.find.mockReturnValue('/tools/packc/v1.0.0');

      const result = await installPackC('v1.0.0');

      expect(result).toBe('/tools/packc/v1.0.0/packc');
    });

    it('should handle arm64 architecture', async () => {
      mockedOs.arch.mockReturnValue('arm64');
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/packc/v1.0.0');

      await installPackC('v1.0.0');

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('Linux_arm64.tar.gz')
      );
    });

    it('should throw on unsupported platform', async () => {
      mockedOs.platform.mockReturnValue('freebsd' as NodeJS.Platform);

      await expect(installPackC('v1.0.0')).rejects.toThrow('Unsupported platform');
    });

    it('should throw on unsupported architecture', async () => {
      mockedOs.arch.mockReturnValue('ia32');

      await expect(installPackC('v1.0.0')).rejects.toThrow('Unsupported architecture');
    });

    it('should set executable permissions on Unix', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/packc/v1.0.0');

      await installPackC('v1.0.0');

      expect(mockedFs.chmodSync).toHaveBeenCalledWith(
        '/tmp/extracted/packc',
        0o755
      );
    });

    it('should not set permissions on Windows', async () => {
      mockedOs.platform.mockReturnValue('win32');
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/packc/v1.0.0');

      await installPackC('v1.0.0');

      expect(mockedFs.chmodSync).not.toHaveBeenCalled();
    });

    it('should throw on failed version fetch', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        statusText: 'Not Found',
      });

      await expect(installPackC('latest')).rejects.toThrow('Failed to fetch latest release');
    });
  });

  describe('installORAS', () => {
    beforeEach(() => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: 'v1.2.0' }),
      });
    });

    it('should install ORAS', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/oras/v1.2.0');

      const result = await installORAS();

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('oras_1.2.0_linux_amd64.tar.gz')
      );
      expect(result).toBe('/tools/oras/v1.2.0/oras');
    });

    it('should use cached ORAS if available', async () => {
      mockedTc.find.mockReturnValue('/tools/oras/v1.2.0');

      const result = await installORAS();

      expect(mockedTc.downloadTool).not.toHaveBeenCalled();
      expect(mockedCore.info).toHaveBeenCalledWith('Found cached ORAS v1.2.0');
      expect(result).toBe('/tools/oras/v1.2.0/oras');
    });

    it('should extract zip on Windows', async () => {
      mockedOs.platform.mockReturnValue('win32');
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.zip');
      mockedTc.extractZip.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/oras/v1.2.0');

      await installORAS();

      expect(mockedTc.extractZip).toHaveBeenCalled();
      expect(mockedTc.extractTar).not.toHaveBeenCalled();
    });

    it('should throw if ORAS binary not found', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedFs.existsSync.mockReturnValue(false);

      await expect(installORAS()).rejects.toThrow('ORAS binary not found');
    });

    it('should throw on failed ORAS version fetch', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        statusText: 'Rate Limited',
      });

      await expect(installORAS()).rejects.toThrow('Failed to fetch latest ORAS release');
    });
  });

  describe('installCosign', () => {
    beforeEach(() => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: 'v2.4.0' }),
      });
    });

    it('should install Cosign', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/cosign');
      mockedTc.cacheDir.mockResolvedValue('/tools/cosign/v2.4.0');

      const result = await installCosign();

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('cosign-linux-amd64')
      );
      expect(mockedFs.mkdirSync).toHaveBeenCalled();
      expect(mockedFs.copyFileSync).toHaveBeenCalled();
      expect(result).toBe('/tools/cosign/v2.4.0/cosign');
    });

    it('should use cached Cosign if available', async () => {
      mockedTc.find.mockReturnValue('/tools/cosign/v2.4.0');

      const result = await installCosign();

      expect(mockedTc.downloadTool).not.toHaveBeenCalled();
      expect(mockedCore.info).toHaveBeenCalledWith('Found cached Cosign v2.4.0');
      expect(result).toBe('/tools/cosign/v2.4.0/cosign');
    });

    it('should handle Windows exe extension', async () => {
      mockedOs.platform.mockReturnValue('win32');
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/cosign.exe');
      mockedTc.cacheDir.mockResolvedValue('/tools/cosign/v2.4.0');

      const result = await installCosign();

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('cosign-windows-amd64.exe')
      );
      expect(result).toBe('/tools/cosign/v2.4.0/cosign.exe');
    });

    it('should throw on failed Cosign version fetch', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        statusText: 'Server Error',
      });

      await expect(installCosign()).rejects.toThrow('Failed to fetch latest Cosign release');
    });
  });
});
