import { test as setup } from '@playwright/test'

import { STORAGE_STATE_PATH } from './auth'

setup('authenticate admin session', async ({ request }) => {
  const response = await request.post('/api/v1/login', {
    data: {
      username: process.env.E2E_ADMIN_USERNAME ?? 'admin',
      password: process.env.E2E_ADMIN_PASSWORD ?? 't11-playwright-password',
    },
  })
  if (!response.ok()) {
    throw new Error(`login for storageState failed: ${response.status()} ${await response.text()}`)
  }
  await request.storageState({ path: STORAGE_STATE_PATH })
})
