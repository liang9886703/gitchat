import test from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { readFile, mkdir } from 'node:fs/promises';
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';

test('connection screen: desktop, mobile, loading, failure, reconnect and keyboard', async t => {
  const server = createServer(async (req, res) => {
    const path = new URL(req.url, 'http://local').pathname;
    const filename = path === '/' ? 'index.html' : path.slice(1);
    if (!['index.html', 'launcher.css', 'launcher.js'].includes(filename)) { res.writeHead(404).end(); return; }
    res.setHeader('Content-Type', filename.endsWith('.js') ? 'text/javascript' : filename.endsWith('.css') ? 'text/css' : 'text/html');
    res.end(await readFile(new URL(`../src/${filename}`, import.meta.url)));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined);
  const browser = await chromium.launch({ executablePath, headless: true });
  t.after(async () => { await browser.close(); await new Promise(resolve => server.close(resolve)); });
  const output = new URL('../test-results/', import.meta.url);
  await mkdir(output, { recursive: true });
  const page = await browser.newPage({ viewport: { width: 1180, height: 820 } });
  await page.addInitScript(() => {
    window.nativeCalls = [];
    window.__TAURI__ = { core: { invoke: async (command, args) => {
      window.nativeCalls.push({ command, args });
      if (command === 'connection_info') return { desktop: !location.search.includes('mobile'), previous: null, error: null };
      await new Promise(resolve => setTimeout(resolve, 100));
      if (args?.url?.includes('fail')) throw '无法连接服务，请检查地址、网络与 HTTPS 证书';
    } } };
  });
  const base = `http://127.0.0.1:${server.address().port}/`;
  await page.goto(base);
  await page.locator('#local-card').waitFor({ state: 'visible' });
  await page.screenshot({ path: fileURLToPath(new URL('desktop-connection.png', output)), fullPage: true });
  await page.locator('#local-button').click();
  await page.waitForFunction(() => document.querySelector('#status').textContent.includes('连接成功'));
  assert.equal(await page.evaluate(() => window.nativeCalls.filter(c => c.command === 'connect_local').length), 1);
  await page.locator('#server-url').fill('https://fail.example.com');
  await page.locator('#server-url').press('Enter');
  await page.waitForFunction(() => document.querySelector('#status').classList.contains('error'));
  assert.equal(await page.locator('#server-url').inputValue(), 'https://fail.example.com');
  await page.locator('#server-url').fill('https://example.com');
  await page.locator('#connect-button').click();
  await page.waitForFunction(() => document.querySelector('#status').textContent.includes('连接成功'));
  assert.deepEqual(await page.evaluate(() => window.nativeCalls.at(-1)), { command: 'connect_remote', args: { url: 'https://example.com' } });
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto(`${base}?mobile=1`);
  assert.equal(await page.locator('#local-card').isVisible(), false);
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  await page.screenshot({ path: fileURLToPath(new URL('mobile-connection.png', output)), fullPage: true });
  await page.locator('#server-url').fill('https://example.com');
  await page.locator('#connect-button').click();
  await page.waitForFunction(() => document.querySelector('#status').textContent.includes('连接成功'));
  const errors = await page.evaluate(() => [...document.querySelectorAll('button,input')].filter(e => e.getBoundingClientRect().height < 44).map(e => e.id));
  assert.deepEqual(errors.filter(id => id !== 'local-button'), []);
});
