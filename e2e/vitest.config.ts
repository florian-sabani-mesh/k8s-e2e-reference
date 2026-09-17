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
  // 'verbose' prints every test's full name and pass/fail outcome, not just a per-file
  // summary, so the log names each scenario (e.g. "rejects an over-balance withdrawal
  // locally without calling Coinbase") as it runs.
  reporters: ['verbose', 'junit'],
  outputFile: { junit: '../artifacts/e2e-junit.xml' },
 },
});
