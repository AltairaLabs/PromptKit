import * as fs from 'node:fs';
import { parsePackFile } from './compiler';

jest.mock('node:fs');

const mockedFs = jest.mocked(fs);

describe('compiler', () => {
  beforeEach(() => {
    jest.clearAllMocks();
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
