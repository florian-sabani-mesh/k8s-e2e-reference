import { defineConfig } from 'vitest/config';
export default defineConfig({
 test: {
  include: ['scenarios/**/*.test.ts'],
  setupFiles: ['support/fixtures.ts'],
  fileParallelism: false,
  maxWorkers: 1,
  testTimeout: 60_000,
  hookTimeout: 30_000,
  retry: 0,
  reporters: ['default', 'junit'],
  outputFile: { junit: '../artifacts/e2e-junit.xml' },
 },
});
