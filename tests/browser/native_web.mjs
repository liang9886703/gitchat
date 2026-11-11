import assert from 'node:assert/strict';
import { ask, idle, session, screenshot, noOverflow } from './helpers.mjs';

// Exercise the actual served UI with the same boolean marker used by native
// hosts. No native process, filesystem bridge or CDP feature is required.
export async function run({ browser, baseURL, outputDir }) {
  const results = [];
  async function scenario(name, work, options = {}) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, reducedMotion: 'reduce', ...options });
    await context.addInitScript(() => {
      window.__GITCHAT_NATIVE__ = true;
      Object.defineProperty(navigator, 'canShare', { configurable: true, value: () => false });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    const errors = [];
    const downloads = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('download', download => downloads.push(download.suggestedFilename()));
    console.log(`  native-web/${name}`);
    try {
      await page.goto(baseURL);
      await page.locator('#question').waitFor();
      await work(page, { context, downloads });
      await noOverflow(page);
      assert.deepEqual(errors, [], 'No uncaught frontend exception');
      results.push({ name: `native-web/${name}`, status: 'passed', screenshot: await screenshot(page, outputDir, `native-web-${name}`) });
    } catch (error) {
      results.push({ name: `native-web/${name}`, status: 'failed', error: error.stack, screenshot: await screenshot(page, outputDir, `native-web-${name}-failed`).catch(() => '') });
    } finally { await context.close(); }
  }
  const nativeRequest = (page, action) => page.evaluate(action => window.dispatchEvent(new CustomEvent('gitchat:native-request', { detail: { action } })), action);
  const compose = (page, operation = 'deepen') => page.locator(`[data-action="compose"][data-value="${operation}"]`).click();
  const cancelGuard = page => page.locator('.dialog-actions [data-action="close-dialog"]').click();
  const openExport = async page => {
    await page.locator('[data-action="session-menu"]').click();
    await page.locator('[data-action="export"]').click();
    await page.locator('#export-json').waitFor();
  };

  await scenario('native-entry-and-unsent-menu-guards', async page => {
    assert.equal(await page.locator('html').evaluate(element => element.classList.contains('native-app')), true);
    assert.equal(await page.locator('[data-platform="install"]').isVisible(), false);
    assert.equal(await page.locator('[data-platform="native-settings"]').isVisible(), true);
    const draft = '还没想完：比较两个方向之后，再选择自己的答案';
    await page.locator('#question').fill(draft);
    await page.evaluate(() => { window.__nativePageIdentity = 'same-renderer'; });
    await nativeRequest(page, 'settings');
    await page.locator('#dialog[open]').waitFor();
    assert.match(await page.locator('#dialog-title').innerText(), /先留住当前的问题/);
    assert.equal(await page.locator('#question').inputValue(), draft);
    assert.equal(page.url(), `${baseURL}/`);
    await cancelGuard(page);
    await nativeRequest(page, 'reload');
    await page.locator('#dialog[open]').waitFor();
    assert.match(await page.locator('#dialog-title').innerText(), /保留问题再刷新/);
    assert.equal(await page.evaluate(() => window.__nativePageIdentity), 'same-renderer', 'Menu refresh must not discard an unsent draft');
    await cancelGuard(page);
    assert.equal(await page.locator('#question').inputValue(), draft);
    await nativeRequest(page, 'reload');
    await Promise.all([page.waitForEvent('load'), page.locator('[data-platform="save-and-reload"]').click()]);
    await page.locator('#question').waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem('gitchat.unsent-inspiration')), draft);
    await page.locator('[data-platform="recover-note"]').click();
    assert.equal(await page.locator('#saved-inspiration').inputValue(), draft);
    await page.locator('[data-platform="use-note"]').click();
    assert.equal(await page.locator('#question').inputValue(), draft);
    assert.equal(await page.locator('#answer-card').count(), 0, 'Recovering a draft must not send it automatically');
  });

  await scenario('native-menu-cannot-interrupt-active-answer', async page => {
    await page.evaluate(() => { window.__nativePageIdentity = 'active-answer'; });
    await page.locator('#question').fill('正在认真探索的方向 [slow]');
    await page.locator('#chat-form button[type="submit"]').click();
    await page.locator('[data-action="stop"]').waitFor();
    for (const action of ['settings', 'reload']) {
      await nativeRequest(page, action);
      await page.waitForFunction(() => document.querySelector('#toast')?.textContent.includes('请先等待当前回答完成'));
      assert.equal(await page.locator('#dialog').evaluate(dialog => dialog.open), false);
      assert.equal(await page.evaluate(() => window.__nativePageIdentity), 'active-answer');
    }
    await idle(page);
    const saved = await session(page);
    assert.equal(saved.cards.at(-1).status, 'completed');
    assert.equal(saved.cards.at(-1).question, '正在认真探索的方向 [slow]');
  });

  await scenario('native-export-is-visible-complete-and-copyable', async (page, { downloads }) => {
    await ask(page, '原生客户端导出完整探索');
    await compose(page); await ask(page, '单独讨论第一条方向');
    await compose(page, 'branch'); await ask(page, '单独讨论第二条方向');
    const saved = await session(page);
    assert.equal(saved.cards.length, 4);
    await openExport(page);
    const text = await page.locator('#export-json').inputValue();
    const exported = JSON.parse(text);
    assert.equal(exported.id, saved.id);
    assert.deepEqual(exported.cards, saved.cards);
    assert.deepEqual(new Set(exported.cards.at(-1).parent_ids), new Set(exported.cards.at(-2).parent_ids));
    assert.equal(await page.locator('[data-platform="share-export"]').count(), 0);
    assert.deepEqual(downloads, [], 'Native export must not silently depend on browser downloads');
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new DOMException('Clipboard denied', 'NotAllowedError'); } } });
    });
    await page.locator('[data-platform="copy-export"]').click();
    await page.waitForFunction(() => document.querySelector('#dialog-title')?.textContent === '复制内容');
    assert.equal(await page.locator('#dialog textarea').inputValue(), text, 'Denied clipboard must still expose the entire export for manual copying');
    assert.deepEqual(downloads, []);
  });

  await scenario('native-export-system-share-and-visible-failure', async page => {
    await ask(page, '可以通过系统保存的探索记录');
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'canShare', { configurable: true, value: ({ files }) => files.length === 1 && files[0].type === 'application/json' });
      Object.defineProperty(navigator, 'share', { configurable: true, value: async ({ files, title }) => {
        window.__sharedNativeFile = { name: files[0].name, text: await files[0].text(), type: files[0].type, title };
      } });
    });
    await openExport(page);
    const text = await page.locator('#export-json').inputValue();
    await page.locator('[data-platform="share-export"]').click();
    await page.waitForFunction(() => !!window.__sharedNativeFile);
    const shared = await page.evaluate(() => window.__sharedNativeFile);
    assert.equal(shared.text, text);
    assert.equal(shared.type, 'application/json');
    assert.equal(shared.name, `gitchat-${JSON.parse(text).id}.json`);
    await page.evaluate(() => { Object.defineProperty(navigator, 'share', { configurable: true, value: async () => { throw new Error('Controlled OS share failure'); } }); });
    await page.locator('[data-platform="share-export"]').click();
    await page.waitForFunction(() => document.querySelector('#dialog-status')?.textContent.includes('系统分享未完成'));
    assert.equal(await page.locator('#export-json').inputValue(), text, 'System-share failure must keep the export visible');
  });

  await scenario('native-restores-latest-space-without-local-session-key', async page => {
    await ask(page, '早一些的探索');
    await page.locator('[data-action="new"]').click();
    await ask(page, '最近的探索，应在桌面服务换端口后继续');
    const latest = await session(page);
    await page.evaluate(() => localStorage.removeItem('gitchat.session'));
    await page.reload();
    await page.locator(`#answer-card[data-card-id="${latest.cards.at(-1).id}"]`).waitFor();
    assert.equal(await page.locator('#page-title').innerText(), latest.title);
    await compose(page); await ask(page, '从原生恢复的背景继续追问');
    const resumed = await page.evaluate(async id => (await fetch(`/api/sessions/${id}`)).json(), latest.id);
    assert.equal(resumed.cards.length, 3);
    assert.equal(resumed.cards.at(-1).question, '从原生恢复的背景继续追问');
    assert.deepEqual(resumed.cards.at(-1).parent_ids, [latest.cards.at(-1).id]);
  });

  // A 640×450 CSS viewport at DPR 2 is the layout/raster equivalent of a
  // 1280×900 desktop viewport at 200% page zoom. Text is independently doubled
  // below to check large-type reading, rather than claiming a native OS setting.
  await scenario('two-hundred-percent-reflow-and-large-reading-text', async page => {
    await ask(page, '放大后也要看清思考路径');
    await compose(page); await ask(page, '放大阅读每一条观察 [long]');
    await page.addStyleTag({ content: '.thought-card h1{font-size:44px!important}.answer{font-size:32px!important;line-height:1.8!important}' });
    await noOverflow(page);
    const reading = await page.locator('#answer-content').evaluate(element => {
      const before = { clientHeight: element.clientHeight, scrollHeight: element.scrollHeight, fontSize: parseFloat(getComputedStyle(element).fontSize) };
      element.scrollTop = element.scrollHeight;
      return { ...before, remaining: element.scrollHeight - element.clientHeight - element.scrollTop };
    });
    assert.ok(reading.fontSize >= 32);
    assert.ok(reading.scrollHeight > reading.clientHeight, 'Long enlarged answer should remain scrollable');
    assert.ok(reading.remaining <= 1, 'The end of an enlarged answer must remain reachable');
    await page.locator('[data-action="compose"][data-value="deepen"]').click();
    await page.locator('#question').fill('放大以后仍能继续提问');
    await page.locator('#chat-form button[type="submit"]').click();
    await idle(page);
    assert.equal((await session(page)).cards.at(-1).question, '放大以后仍能继续提问');
  }, { viewport: { width: 640, height: 450 }, deviceScaleFactor: 2 });

  await scenario('short-native-phone-window-keeps-dialog-and-send-reachable', async page => {
    await ask(page, '手机上的短窗口探索');
    await compose(page);
    await page.locator('#question').fill('键盘上方保留的问题');
    await nativeRequest(page, 'settings');
    await page.locator('#dialog[open]').waitFor();
    assert.match(await page.locator('#dialog-title').innerText(), /先留住/);
    await cancelGuard(page);
    assert.equal(await page.locator('#question').inputValue(), '键盘上方保留的问题');
    await page.locator('#chat-form button[type="submit"]').click(); await idle(page);
    assert.equal((await session(page)).cards.at(-1).question, '键盘上方保留的问题');
    await openExport(page);
    const bounds = await page.locator('#dialog').boundingBox();
    const height = await page.evaluate(() => innerHeight);
    assert.ok(bounds.y >= 0 && bounds.y + bounds.height <= height + 1, 'Dialog must fit the short visual viewport');
    await page.locator('[data-platform="copy-export"]').scrollIntoViewIfNeeded();
    assert.equal(await page.locator('[data-platform="copy-export"]').isVisible(), true);
  }, { viewport: { width: 375, height: 380 }, isMobile: true, hasTouch: true });

  return results;
}
