import { defineConfig } from '@playwright/test'

import { STORAGE_STATE_PATH } from './e2e/auth'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'https://127.0.0.1:18443',
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: 'setup',
      testMatch: /auth\.setup\.ts/,
    },
    {
      name: 'chromium',
      testMatch: /.*\.spec\.ts/,
      dependencies: ['setup'],
      use: { storageState: STORAGE_STATE_PATH },
    },
  ],
})
