import * as core from '@actions/core';
import * as tc from '@actions/tool-cache';
import * as fs from 'node:fs';
import * as os from 'node:os';
import { installPromptArena } from './installer';

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
    mockedFs.existsSync.mockReturnValue(true);
  });

  describe('installPromptArena', () => {
    it('should install promptarena with specific version', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      const result = await installPromptArena('v1.0.0');

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('PromptKit_1.0.0_Linux_x86_64.tar.gz')
      );
      expect(mockedTc.cacheDir).toHaveBeenCalled();
      expect(mockedCore.addPath).toHaveBeenCalledWith('/tools/promptarena/v1.0.0');
      expect(result).toBe('/tools/promptarena/v1.0.0/promptarena');
    });

    it('should use cached version if available', async () => {
      mockedTc.find.mockReturnValue('/tools/promptarena/v1.0.0');

      const result = await installPromptArena('v1.0.0');

      expect(mockedTc.downloadTool).not.toHaveBeenCalled();
      expect(mockedCore.info).toHaveBeenCalledWith('Found cached promptarena v1.0.0');
      expect(result).toBe('/tools/promptarena/v1.0.0/promptarena');
    });

    it('should resolve latest version', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ tag_name: 'v2.0.0' }),
      });
      mockedTc.find.mockReturnValue('/tools/promptarena/v2.0.0');

      await installPromptArena('latest');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('AltairaLabs/PromptKit/releases/latest'),
        expect.any(Object)
      );
      expect(mockedCore.info).toHaveBeenCalledWith('Resolving latest version...');
      expect(mockedCore.info).toHaveBeenCalledWith('Latest version is v2.0.0');
    });

    it('should add v prefix if missing', async () => {
      mockedTc.find.mockReturnValue('/tools/promptarena/v1.0.0');

      await installPromptArena('1.0.0');

      expect(mockedTc.find).toHaveBeenCalledWith('promptarena', 'v1.0.0', 'x86_64');
    });

    it('should throw if binary not found after extraction', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedFs.existsSync.mockReturnValue(false);

      await expect(installPromptArena('v1.0.0')).rejects.toThrow('Binary not found');
    });

    it('should throw if cached binary not found', async () => {
      mockedTc.find.mockReturnValue('/tools/promptarena/v1.0.0');
      mockedFs.existsSync.mockReturnValue(false);

      await expect(installPromptArena('v1.0.0')).rejects.toThrow('promptarena binary not found');
    });

    it('should handle Windows platform', async () => {
      mockedOs.platform.mockReturnValue('win32');
      mockedTc.find.mockReturnValue('/tools/promptarena/v1.0.0');

      const result = await installPromptArena('v1.0.0');

      expect(result).toBe('/tools/promptarena/v1.0.0/promptarena.exe');
    });

    it('should handle Darwin platform', async () => {
      mockedOs.platform.mockReturnValue('darwin');
      mockedTc.find.mockReturnValue('/tools/promptarena/v1.0.0');

      const result = await installPromptArena('v1.0.0');

      expect(result).toBe('/tools/promptarena/v1.0.0/promptarena');
    });

    it('should handle arm64 architecture', async () => {
      mockedOs.arch.mockReturnValue('arm64');
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      await installPromptArena('v1.0.0');

      expect(mockedTc.downloadTool).toHaveBeenCalledWith(
        expect.stringContaining('Linux_arm64.tar.gz')
      );
    });

    it('should throw on unsupported platform', async () => {
      mockedOs.platform.mockReturnValue('freebsd' as NodeJS.Platform);

      await expect(installPromptArena('v1.0.0')).rejects.toThrow('Unsupported platform');
    });

    it('should throw on unsupported architecture', async () => {
      mockedOs.arch.mockReturnValue('ia32');

      await expect(installPromptArena('v1.0.0')).rejects.toThrow('Unsupported architecture');
    });

    it('should set executable permissions on Unix', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      await installPromptArena('v1.0.0');

      expect(mockedFs.chmodSync).toHaveBeenCalledWith(
        '/tmp/extracted/promptarena',
        0o755
      );
    });

    it('should not set permissions on Windows', async () => {
      mockedOs.platform.mockReturnValue('win32');
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      await installPromptArena('v1.0.0');

      expect(mockedFs.chmodSync).not.toHaveBeenCalled();
    });

    it('should throw on failed version fetch', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        statusText: 'Not Found',
      });

      await expect(installPromptArena('latest')).rejects.toThrow('Failed to fetch latest release');
    });

    it('should log download URL', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      await installPromptArena('v1.0.0');

      expect(mockedCore.info).toHaveBeenCalledWith(
        expect.stringContaining('Download URL: https://github.com/AltairaLabs/PromptKit/releases/download')
      );
    });

    it('should log extraction step', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      await installPromptArena('v1.0.0');

      expect(mockedCore.info).toHaveBeenCalledWith('Extracting archive...');
    });

    it('should log caching step', async () => {
      mockedTc.find.mockReturnValue('');
      mockedTc.downloadTool.mockResolvedValue('/tmp/archive.tar.gz');
      mockedTc.extractTar.mockResolvedValue('/tmp/extracted');
      mockedTc.cacheDir.mockResolvedValue('/tools/promptarena/v1.0.0');

      await installPromptArena('v1.0.0');

      expect(mockedCore.info).toHaveBeenCalledWith('Cached promptarena to /tools/promptarena/v1.0.0');
    });
  });
});
