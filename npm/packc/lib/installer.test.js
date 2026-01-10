import {
  getPlatformInfo,
  getDownloadUrl,
  PLATFORM_MAP,
  ARCH_MAP
} from './installer.js';

describe('installer', () => {
  describe('PLATFORM_MAP', () => {
    it('should map darwin to Darwin', () => {
      expect(PLATFORM_MAP.darwin).toBe('Darwin');
    });

    it('should map linux to Linux', () => {
      expect(PLATFORM_MAP.linux).toBe('Linux');
    });

    it('should map win32 to Windows', () => {
      expect(PLATFORM_MAP.win32).toBe('Windows');
    });
  });

  describe('ARCH_MAP', () => {
    it('should map x64 to x86_64', () => {
      expect(ARCH_MAP.x64).toBe('x86_64');
    });

    it('should map arm64 to arm64', () => {
      expect(ARCH_MAP.arm64).toBe('arm64');
    });
  });

  describe('getPlatformInfo', () => {
    it('should return mapped platform and arch for darwin x64', () => {
      const result = getPlatformInfo('darwin', 'x64');
      expect(result).toEqual({ platform: 'Darwin', arch: 'x86_64' });
    });

    it('should return mapped platform and arch for darwin arm64', () => {
      const result = getPlatformInfo('darwin', 'arm64');
      expect(result).toEqual({ platform: 'Darwin', arch: 'arm64' });
    });

    it('should return mapped platform and arch for linux x64', () => {
      const result = getPlatformInfo('linux', 'x64');
      expect(result).toEqual({ platform: 'Linux', arch: 'x86_64' });
    });

    it('should return mapped platform and arch for linux arm64', () => {
      const result = getPlatformInfo('linux', 'arm64');
      expect(result).toEqual({ platform: 'Linux', arch: 'arm64' });
    });

    it('should return mapped platform and arch for win32 x64', () => {
      const result = getPlatformInfo('win32', 'x64');
      expect(result).toEqual({ platform: 'Windows', arch: 'x86_64' });
    });

    it('should throw error for unsupported platform', () => {
      expect(() => getPlatformInfo('freebsd', 'x64')).toThrow(
        'Unsupported platform: freebsd-x64'
      );
    });

    it('should throw error for unsupported arch', () => {
      expect(() => getPlatformInfo('darwin', 'ia32')).toThrow(
        'Unsupported platform: darwin-ia32'
      );
    });

    it('should throw error for unsupported platform and arch', () => {
      expect(() => getPlatformInfo('aix', 'ppc64')).toThrow(
        'Unsupported platform: aix-ppc64'
      );
    });
  });

  describe('getDownloadUrl', () => {
    it('should generate correct URL for Darwin x86_64', () => {
      const url = getDownloadUrl('1.0.0', 'Darwin', 'x86_64');
      expect(url).toBe(
        'https://github.com/AltairaLabs/PromptKit/releases/download/v1.0.0/PromptKit_1.0.0_Darwin_x86_64.tar.gz'
      );
    });

    it('should generate correct URL for Darwin arm64', () => {
      const url = getDownloadUrl('1.0.0', 'Darwin', 'arm64');
      expect(url).toBe(
        'https://github.com/AltairaLabs/PromptKit/releases/download/v1.0.0/PromptKit_1.0.0_Darwin_arm64.tar.gz'
      );
    });

    it('should generate correct URL for Linux x86_64', () => {
      const url = getDownloadUrl('2.1.0', 'Linux', 'x86_64');
      expect(url).toBe(
        'https://github.com/AltairaLabs/PromptKit/releases/download/v2.1.0/PromptKit_2.1.0_Linux_x86_64.tar.gz'
      );
    });

    it('should generate correct URL for Windows (zip extension)', () => {
      const url = getDownloadUrl('1.0.0', 'Windows', 'x86_64');
      expect(url).toBe(
        'https://github.com/AltairaLabs/PromptKit/releases/download/v1.0.0/PromptKit_1.0.0_Windows_x86_64.zip'
      );
    });

    it('should use custom repository when provided', () => {
      const url = getDownloadUrl('1.0.0', 'Darwin', 'x86_64', 'MyOrg/MyRepo');
      expect(url).toBe(
        'https://github.com/MyOrg/MyRepo/releases/download/v1.0.0/PromptKit_1.0.0_Darwin_x86_64.tar.gz'
      );
    });

    it('should handle prerelease versions', () => {
      const url = getDownloadUrl('1.0.0-beta.1', 'Linux', 'arm64');
      expect(url).toBe(
        'https://github.com/AltairaLabs/PromptKit/releases/download/v1.0.0-beta.1/PromptKit_1.0.0-beta.1_Linux_arm64.tar.gz'
      );
    });
  });
});
