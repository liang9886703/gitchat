import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { ask, idle, session, screenshot, noOverflow } from './helpers.mjs';

export async function run({ browser, baseURL, outputDir }) {
  const results = [];
  async function scenario(name, work) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce', permissions: ['clipboard-read', 'clipboard-write'] });
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    page.setDefaultNavigationTimeout(12000);
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    try {
      await page.goto(baseURL);
      await page.waitForSelector('#question');
      await work(page, context);
      await noOverflow(page);
      assert.deepEqual(errors, []);
      results.push({ name: `desktop/${name}`, status: 'passed', screenshot: await screenshot(page, outputDir, `desktop-${name}`) });
    } catch (error) {
      results.push({ name: `desktop/${name}`, status: 'failed', error: error.stack, screenshot: await screenshot(page, outputDir, `desktop-${name}`).catch(() => '') });
    } finally { await context.close(); }
  }
  async function action(page, name) { await page.locator(`[data-action="${name}"]`).first().click(); }
  async function compose(page, operation = 'deepen') { await page.locator(`[data-action="compose"][data-value="${operation}"]`).first().click(); }
  async function checkpoint(page, name) { results.push({ name: `desktop/${name}`, status: 'passed', screenshot: await screenshot(page, outputDir, `desktop-${name}`) }); }
  async function newChat(page, question) { await action(page, 'new'); await ask(page, question); return session(page); }

  await scenario('welcome-starters-keyboard-and-double-submit', async page => {
    for (let i = 0; i < 3; i++) {
      const starter = page.locator('[data-action="starter"]').nth(i);
      const question = await starter.getAttribute('data-value');
      await starter.click();
      assert.equal(await page.locator('#question').inputValue(), question);
    }
    await page.locator('#question').fill('');
    await page.locator('#chat-form button[type="submit"]').click();
    assert.match(await page.locator('#toast').innerText(), /先写下/);
    await page.locator('#question').fill('第一行');
    await page.locator('#question').press('Shift+Enter');
    await page.locator('#question').pressSequentially('second line');
    assert.equal(await page.locator('#question').inputValue(), '第一行\nsecond line');
    await page.locator('#question').dispatchEvent('keydown', { key: 'Enter', isComposing: true });
    assert.equal(await page.locator('#question').count(), 1);
    await page.locator('#chat-form').evaluate(form => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    await idle(page);
    const saved = await session(page);
    assert.equal(saved.cards.length, 2);
    assert.equal(saved.cards[1].question, '第一行\nsecond line');
  });

  await scenario('compose-quote-toggle-clear-and-enter', async page => {
    await ask(page, '引用与追问');
    await page.locator('#answer-content p').first().evaluate(element => {
      const range = document.createRange(); range.selectNodeContents(element);
      const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range);
      document.dispatchEvent(new Event('selectionchange'));
    });
    await action(page, 'quote');
    assert.ok((await page.locator('.quote').innerText()).includes('明确目标'));
    await checkpoint(page, 'selected-text-in-composer');
    await page.locator('#question').fill('带着草稿切换');
    await page.locator('[data-action="operation"][data-value="branch"]').click();
    assert.equal(await page.locator('#question').inputValue(), '带着草稿切换');
    await page.locator('[data-action="operation"][data-value="deepen"]').click();
    await action(page, 'clear-quote');
    assert.equal(await page.locator('.quote').count(), 0);
    await action(page, 'cancel-compose');
    await compose(page);
    assert.equal(await page.locator('#question').inputValue(), '带着草稿切换');
    await page.locator('#question').press('Enter');
    await idle(page);
    const saved = await session(page);
    assert.equal(saved.cards.at(-1).question, '带着草稿切换');
    assert.equal(saved.cards.at(-1).quote || '', '');
  });

  await scenario('graph-navigation-predictions-and-merge', async page => {
    await ask(page, '比较三种架构');
    const first = (await session(page)).cards.at(-1);
    await page.locator('[data-action="suggestion"]').first().click();
    await idle(page);
    const child = (await session(page)).cards.at(-1);
    await compose(page, 'branch');
    await ask(page, '另一个独立方案');
    const sibling = (await session(page)).cards.at(-1);
    assert.deepEqual(sibling.parent_ids, child.parent_ids);
    await page.locator('.breadcrumb [data-action="focus"]').click();
    await page.waitForFunction(title => document.querySelector('#answer-card h1')?.textContent === title, first.title);
    await page.locator('.view-switch [data-value="overview"]').click();
    await page.locator(`[data-action="graph-focus"][data-id="${child.id}"]`).click();
    await page.waitForFunction(title => document.querySelector('#answer-card h1')?.textContent === title, child.title);
    await action(page, 'parent');
    await page.waitForFunction(title => document.querySelector('#answer-card h1')?.textContent === title, first.title);
    await page.locator('.view-switch [data-value="overview"]').click();
    assert.equal(await page.locator('[data-action="merge"]').isDisabled(), true);
    await page.locator(`[data-action="select"][data-id="${child.id}"]`).check();
    await page.locator(`[data-action="select"][data-id="${sibling.id}"]`).check();
    assert.equal(await page.locator('[data-action="merge"]').isDisabled(), false);
    await action(page, 'clear-selection');
    assert.equal(await page.locator('[data-action="select"]:checked').count(), 0);
    const graphNode = page.locator(`[data-action="graph-focus"][data-id="${child.id}"]`);
    await graphNode.focus(); await page.keyboard.press('Enter');
    await page.waitForSelector('#answer-card');
    await action(page, 'sibling');
    await page.waitForFunction(title => document.querySelector('#answer-card h1')?.textContent === title, sibling.title);
    await page.locator('.view-switch [data-value="overview"]').click();
    await action(page, 'center-graph');
    await page.locator(`[data-action="select"][data-id="${child.id}"]`).check();
    await page.locator(`[data-action="select"][data-id="${sibling.id}"]`).check();
    await checkpoint(page, 'graph-selected-branches');
    await action(page, 'merge');
    assert.equal(await page.locator('.context-chip').count(), 2);
    await ask(page, '合并两条思路');
    const merged = (await session(page)).cards.at(-1);
    assert.deepEqual(new Set(merged.parent_ids), new Set([child.id, sibling.id]));
    await page.locator(`.path-rail [data-action="focus"][data-id="${first.id}"]`).click();
    await page.waitForFunction(title => document.querySelector('#answer-card h1')?.textContent === title, first.title);
    await page.locator('.view-switch [data-value="overview"]').click();
    await page.locator(`[data-action="graph-focus"][data-id="${merged.id}"]`).click();
    await page.waitForFunction(title => document.querySelector('#answer-card h1')?.textContent === title, merged.title);
    await page.locator('.view-switch [data-value="overview"]').click();
    await page.locator('.ghost').first().click();
    await idle(page);
    assert.deepEqual((await session(page)).cards.at(-1).parent_ids, [merged.id]);
  });

  await scenario('favorites-success-and-empty-collections', async page => {
    await page.locator('[data-action="collection"][data-value="favorites"]').click();
    await page.waitForSelector('#main .empty');
    assert.match(await page.locator('#main').innerText(), /还没有收藏/);
    await action(page, 'new');
    await ask(page, '值得保留的结论');
    await action(page, 'favorite');
    await page.waitForSelector('[data-action="favorite"].selected');
    await action(page, 'success');
    await page.waitForFunction(() => document.querySelector('[data-action="success"]')?.textContent.includes('已找到答案'));
    await action(page, 'success');
    await page.waitForFunction(() => document.querySelector('[data-action="success"]')?.textContent.includes('这个问题想清楚了'));
    await page.locator('[data-action="collection"][data-value="favorites"]').click();
    await page.waitForSelector('.collection-card');
    await checkpoint(page, 'favorite-collection');
    await action(page, 'collection-open');
    await page.waitForSelector('#answer-card');
    await page.locator('[data-action="collection"][data-value="favorites"]').click();
    await action(page, 'collection-unfavorite');
    await page.waitForSelector('#main .empty');
    assert.equal((await session(page)).cards.at(-1).favorite, false);
  });

  await scenario('archive-survives-reload-and-restores', async page => {
    await ask(page, '归档后应隐藏的卡片');
    const saved = await session(page);
    await action(page, 'card-menu'); await action(page, 'archive');
    await page.waitForFunction(() => !document.querySelector('#dialog').open);
    await page.reload();
    await page.waitForSelector('#question');
    assert.equal(await page.locator('#answer-card').count(), 0);
    await page.locator('[data-action="collection"][data-value="archive"]').click();
    await page.waitForSelector('.collection-card');
    await checkpoint(page, 'archived-card-hidden-until-restored');
    await action(page, 'collection-open');
    await page.waitForSelector('#answer-card');
    assert.equal((await session(page)).cards.find(card => card.id === saved.cards[1].id).archived, false);
    await page.reload(); await page.waitForSelector('#answer-card');
  });

  await scenario('copy-clipboard-export-and-delete', async page => {
    await ask(page, '复制与导出');
    const original = await session(page);
    await action(page, 'card-menu'); await action(page, 'copy-answer');
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), original.cards[1].answer);
    await page.evaluate(() => { navigator.clipboard.writeText = async () => { throw new Error('test denied'); }; });
    await action(page, 'card-menu'); await action(page, 'copy-answer');
    await page.waitForSelector('#dialog textarea');
    assert.equal(await page.locator('#dialog textarea').inputValue(), original.cards[1].answer);
    await checkpoint(page, 'clipboard-fallback');
    await action(page, 'close-dialog');
    await action(page, 'card-menu'); await action(page, 'copy-node');
    await page.waitForFunction(id => localStorage.getItem('gitchat.session') !== id, original.id);
    const copy = await session(page); assert.notEqual(copy.id, original.id); assert.notEqual(copy.cards[1].id, original.cards[1].id);
    await action(page, 'session-menu');
    const downloadPromise = page.waitForEvent('download');
    await action(page, 'export');
    const download = await downloadPromise;
    await download.saveAs(join(outputDir, 'exported-session.json'));
    assert.equal(JSON.parse(await readFile(join(outputDir, 'exported-session.json'), 'utf8')).id, copy.id);
    await action(page, 'session-menu'); await action(page, 'delete-dialog');
    await page.locator('#dialog .secondary').click();
    assert.equal((await session(page)).id, copy.id);
    await action(page, 'session-menu'); await action(page, 'delete-dialog'); await action(page, 'delete-session');
    await page.waitForSelector('#question');
    await page.waitForFunction(() => document.querySelectorAll('.session-item[data-action]').length === 1);
  });

  await scenario('share-link-token-import-and-readonly', async (page, context) => {
    await ask(page, '分享的独立语境');
    await action(page, 'card-menu'); await action(page, 'share');
    const url = await page.locator('#share-url').inputValue();
    await action(page, 'copy-share');
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), url);
    await checkpoint(page, 'share-link-dialog');
    await action(page, 'close-dialog');
    await action(page, 'import');
    await page.locator('#import-token').fill(new URL(url).searchParams.get('share'));
    await page.locator('#import-form button[type="submit"]').click();
    await page.waitForFunction(() => !document.querySelector('#dialog').open);
    const imported = await session(page);
    await action(page, 'import'); await page.locator('#import-token').fill(url);
    await page.locator('#import-form button[type="submit"]').click();
    await page.waitForFunction(id => localStorage.getItem('gitchat.session') !== id, imported.id);
    await page.goto(url); await page.waitForSelector('.shared-banner');
    assert.equal(await page.locator('[data-action="favorite"]').isDisabled(), true);
    assert.equal(await page.locator('[data-action="compose"]').count(), 0);
    await page.locator('.view-switch [data-value="overview"]').click();
    assert.equal(await page.locator('[data-action="select"]').count(), 0);
    await checkpoint(page, 'shared-readonly-overview');
  });

  await scenario('preferences-validation-and-all-dialog-exits', async page => {
    await action(page, 'settings');
    await page.locator('#tone').fill('简洁直接');
    await page.locator('#interests').fill('技术，旅行');
    await page.locator('#tasks').fill('方案对比\n概念学习');
    await page.locator('#learned').fill('x'.repeat(501));
    await page.locator('#preferences-form button[type="submit"]').click();
    await page.waitForSelector('#dialog-status:not([hidden])');
    await checkpoint(page, 'visible-dialog-validation');
    await page.locator('#learned').fill('喜欢例子\n偏好短回答');
    await page.locator('#preferences-form button[type="submit"]').click();
    await page.waitForFunction(() => !document.querySelector('#dialog').open);
    await action(page, 'settings');
    assert.equal(await page.locator('#tone').inputValue(), '简洁直接');
    assert.equal(await page.locator('#learned').inputValue(), '喜欢例子\n偏好短回答');
    await page.locator('#learned').fill('');
    await page.locator('#preferences-form button[type="submit"]').click();
    await page.waitForFunction(() => !document.querySelector('#dialog').open);
    await action(page, 'help'); await page.keyboard.press('Escape');
    assert.equal(await page.locator('#dialog').evaluate(dialog => dialog.open), false);
    await action(page, 'help'); await page.mouse.click(3, 3);
    assert.equal(await page.locator('#dialog').evaluate(dialog => dialog.open), false);
    await action(page, 'help'); await checkpoint(page, 'gesture-help'); await action(page, 'close-dialog');
    const preferences = await page.evaluate(() => fetch('/api/preferences').then(response => response.json()));
    assert.deepEqual(preferences.learned || [], []);
  });

  await scenario('late-session-response-cannot-replace-new-work', async page => {
    const first = await newChat(page, '较早的会话');
    await newChat(page, '当前的会话');
    let release;
    const gate = new Promise(resolve => { release = resolve; });
    let entered;
    const blocked = new Promise(resolve => { entered = resolve; });
    await page.route(`**/api/sessions/${first.id}`, async route => {
      const response = await route.fetch(); entered(); await gate; await route.fulfill({ response });
    });
    await page.locator(`[data-action="session"][data-id="${first.id}"]`).click();
    await blocked;
    await action(page, 'new');
    await page.locator('#question').fill('[slow] 新的工作不应被旧响应覆盖');
    await page.locator('#chat-form button[type="submit"]').click();
    release();
    await idle(page);
    assert.equal((await session(page)).cards.at(-1).question, '[slow] 新的工作不应被旧响应覆盖');
    assert.match(await page.locator('#answer-card').innerText(), /新的工作/);
  });

  await scenario('late-archive-response-cannot-replace-favorites', async page => {
    await ask(page, '保留在收藏中的卡片'); await action(page, 'favorite');
    await page.waitForSelector('[data-action="favorite"].selected');
    await compose(page); await ask(page, '只出现在归档中的卡片');
    await action(page, 'card-menu'); await action(page, 'archive');
    await page.waitForFunction(() => !document.querySelector('#dialog').open);
    const saved = await session(page);
    let release; const gate = new Promise(resolve => { release = resolve; });
    let entered; const blocked = new Promise(resolve => { entered = resolve; });
    let delayed = false;
    await page.route(`**/api/sessions/${saved.id}`, async route => {
      const response = await route.fetch();
      if (!delayed) { delayed = true; entered(); await gate; }
      await route.fulfill({ response });
    });
    await page.locator('[data-action="collection"][data-value="archive"]').click(); await blocked;
    await page.locator('[data-action="collection"][data-value="favorites"]').click();
    await page.waitForSelector('.collection-card'); release();
    await page.waitForTimeout(150);
    assert.match(await page.locator('.collection-card h2').innerText(), /保留在收藏/);
    assert.doesNotMatch(await page.locator('#main').innerText(), /只出现在归档/);
  });

  await scenario('root-topic-draft-and-session-switch', async page => {
    await ask(page, '第一个根话题'); const first = await session(page);
    await page.locator('.view-switch [data-value="overview"]').click(); await action(page, 'compose-root');
    await page.locator('[data-action="starter"]').last().click();
    await ask(page, '第二个根话题');
    const saved = await session(page);
    assert.deepEqual(saved.cards.at(-1).parent_ids, [first.root_id]);
    await action(page, 'new');
    await page.locator(`[data-action="session"][data-id="${saved.id}"]`).click();
    await page.waitForSelector('#answer-card');
    assert.equal((await session(page)).id, saved.id);
    await page.reload(); await page.waitForSelector('#answer-card');
  });
  return results;
}
