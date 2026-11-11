import { chromium, webkit } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve, join, basename } from 'node:path';
import { startService, browserOptions } from './fixture.mjs';

const outputDir = resolve(process.env.GITCHAT_TEST_OUTPUT || 'test-results');
await mkdir(outputDir, { recursive: true });
const args = process.argv.slice(2);
const isWebKit = args.includes('--webkit');
const selected = args.filter(arg => arg !== '--webkit');
const suites = selected.length ? selected : ['desktop', 'mobile', 'recovery', 'experience', 'platform', 'native_web', 'models'];
let service, browser;
const results = [];
try {
  service = process.env.GITCHAT_TEST_URL ? { baseURL: process.env.GITCHAT_TEST_URL, close: async () => {} } : await startService();
  browser = isWebKit ? await webkit.launch({headless: process.env.HEADED !== '1'}) : await chromium.launch(await browserOptions());
  for (const name of suites) {
    console.log(`Running ${name} at ${service.baseURL}`);
    try {
      const { run } = await import(`./${name}.mjs`);
      results.push(...await run({ browser, baseURL: service.baseURL, modelURL: service.modelURL, outputDir }));
    } catch (error) {
      results.push({ name, status: 'failed', error: error.stack });
    }
  }
} finally {
  await browser?.close();
  await service?.close();
}
for (const result of results) { if (result.screenshot) result.screenshot = basename(result.screenshot); }
const report = { browser: isWebKit ? 'WebKit' : 'Chromium', date: new Date().toISOString(), localDate: new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'full', timeStyle: 'long' }).format(new Date()), results };
await writeFile(join(outputDir, 'results.json'), JSON.stringify(report, null, 2));
const escape = value => String(value || '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
await writeFile(join(outputDir, 'report.html'), `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>GitChat 交互验证</title><style>body{font:16px system-ui;background:#f7f7fb;color:#252438;margin:32px auto;max-width:1100px;padding:0 20px}article{padding:24px;background:white;border-radius:16px;margin:20px 0}img{max-width:100%;max-height:800px;object-fit:contain}pre{white-space:pre-wrap}.passed{color:#26704b}.failed{color:#a02a36}</style><h1>GitChat 浏览器交互验证</h1><p>${escape(report.localDate)} · ${results.filter(r=>r.status==='passed').length} 项通过 / ${results.length} 项</p>${results.map(r=>`<article><h2 class="${escape(r.status)}">${escape(r.status)} · ${escape(r.name)}</h2>${r.error?`<pre>${escape(r.error)}</pre>`:''}${r.screenshot?`<a href="${escape(r.screenshot)}"><img src="${escape(r.screenshot)}" alt="${escape(r.name)}"></a>`:''}</article>`).join('')}</html>`);
console.log(JSON.stringify(report, null, 2));
if (results.some(r => r.status !== 'passed')) process.exitCode = 1;
