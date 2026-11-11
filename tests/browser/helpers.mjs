import assert from 'node:assert/strict';
import { join } from 'node:path';

export async function idle(page) {
  await page.waitForFunction(() => !document.querySelector('[data-action="stop"]') && document.querySelector('#answer-card .card-bottom'), null, { timeout: 15000 });
}
export async function ask(page, question) {
  await page.locator('#question').fill(question);
  await page.locator('#chat-form button[type="submit"]').click();
  await idle(page);
}
export async function session(page) {
  return page.evaluate(async () => {
    const id = localStorage.getItem('gitchat.session');
    const response = await fetch(`/api/sessions/${id}`);
    if (!response.ok) throw new Error('Cannot read active session');
    return response.json();
  });
}
export async function screenshot(page, outputDir, name) {
  const file = `${name}.png`;
  await page.screenshot({ path: join(outputDir, file), fullPage: true });
  return file;
}
export async function noOverflow(page) {
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'Page overflows viewport');
}
