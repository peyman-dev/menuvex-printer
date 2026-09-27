import { configDefaults, defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    // Packaging tests use node:test and run separately in the installer workflow.
    // Loading them in Vitest reports "No test suite found" despite valid Node tests.
    exclude: [...configDefaults.exclude, 'scripts/tests/**'],
  },
});
