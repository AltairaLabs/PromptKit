export default {
  testEnvironment: 'node',
  testMatch: ['**/*.test.js'],
  collectCoverageFrom: [
    '*.js',
    'bin/*.js',
    'lib/*.js',
    '!jest.config.js',
    '!**/*.test.js',
    '!coverage/**',
  ],
  coverageDirectory: 'coverage',
  coverageReporters: ['text', 'lcov', 'cobertura'],
  transform: {},
  moduleFileExtensions: ['js', 'mjs', 'json', 'node'],
};
