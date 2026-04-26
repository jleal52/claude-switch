import { test, expect } from '@playwright/test';

test('login screen renders provider buttons', async ({ page }) => {
  await page.route('**/api/me', (r) => r.fulfill({
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify({ providers_configured: ['github', 'google'] }),
  }));

  await page.goto('/login');
  await expect(page.getByRole('link', { name: /github/i })).toBeVisible();
  await expect(page.getByRole('link', { name: /google/i })).toBeVisible();
});
