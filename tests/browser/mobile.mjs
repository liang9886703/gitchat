import assert from 'node:assert/strict';
import path from 'node:path';
import fs from 'node:fs/promises';

export async function run({ browser, baseURL, outputDir }) {
  await fs.mkdir(outputDir, { recursive: true });
  const results = [];
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 1,
    isMobile: true,
    hasTouch: true,
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.setDefaultNavigationTimeout(12000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const touch = await context.newCDPSession(page);
  let checkpoint = 0;

  async function capture(name) {
    const file = path.join(outputDir, `mobile-${String(++checkpoint).padStart(2, '0')}-${name}.png`);
    await page.screenshot({ path: file, fullPage: true });
    return file;
  }
  async function step(name, action) {
    try {
      await action();
      results.push({ name: `mobile / ${name}`, status: 'passed', screenshot: await capture(name) });
      return true;
    } catch (error) {
      results.push({ name: `mobile / ${name}`, status: 'failed', error: error.message, screenshot: await capture(name) });
      return false;
    }
  }
  async function noOverflow() {
    const dimensions = await page.evaluate(() => ({ width: innerWidth, content: document.documentElement.scrollWidth }));
    assert.ok(dimensions.content <= dimensions.width + 1, `Horizontal overflow ${JSON.stringify(dimensions)}`);
  }
  async function completed() {
    await page.waitForFunction(() => !document.querySelector('[data-action="stop"]') &&
      document.querySelector('#answer-card .card-bottom')?.textContent.includes('已完成'), null, { timeout: 15000 });
  }
  async function send(question) {
    await page.locator('#question').fill(question);
    await page.locator('#chat-form button[type="submit"]').click();
    await completed();
  }
  async function session() {
    return page.evaluate(async () => {
      const sessions = await fetch('/api/sessions').then(response => response.json());
      return fetch(`/api/sessions/${sessions[0].id}`).then(response => response.json());
    });
  }
  async function swipe(dx, dy, target = '#answer-card h1') {
    await page.locator(target).scrollIntoViewIfNeeded();
    const box = await page.locator(target).boundingBox();
    assert.ok(box, `Missing gesture target ${target}`);
    const x = Math.max(35, Math.min(355, box.x + box.width / 2));
    const y = Math.max(140, Math.min(550, box.y + box.height / 2));
    await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
    for (let i = 1; i <= 4; i++) {
      await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + dx * i / 4, y: y + dy * i / 4 }] });
      await new Promise(resolve => setTimeout(resolve, 30));
    }
    await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  }
  async function focusByGraph(cardID) {
    await page.locator('.view-switch [data-value="overview"]').click();
    await page.locator(`[data-action="graph-focus"][data-id="${cardID}"]`).click();
    await page.waitForSelector('#answer-card');
  }

  try {
    await page.goto(baseURL);
    await page.waitForSelector('#question');
    await step('welcome-390', noOverflow);
    await step('sidebar-and-scrim', async () => {
      await page.locator('.mobile-menu').click();
      assert.ok(await page.locator('#sidebar.open').isVisible());
      await page.locator('#sidebar-scrim').click({ position: { x: 350, y: 150 } });
      await page.waitForFunction(() => !document.querySelector('#sidebar.open'));
      await page.locator('.mobile-menu').click();
      await page.locator('[data-action="settings"]').click();
      await page.waitForSelector('#preferences-form');
      assert.equal(await page.locator('#sidebar.open').count(), 0);
      await page.locator('[data-action="close-dialog"]').click();
    });
    await page.locator('[data-action="starter"]').first().click();
    await send('手机交互测试，如何规划一次旅行？');
    const initial = await session();
    const first = initial.cards.find(card => card.id !== initial.root_id);
    await step('answer-card-390', noOverflow);

    const upward = await step('swipe-up-deepen', async () => {
      await swipe(0, -110);
      await page.waitForSelector('.compose-card');
      assert.match(await page.locator('.compose-card .depth-label').innerText(), /深入/);
    });
    if (!upward) await page.locator('[data-action="compose"][data-value="deepen"]').click();
    await send('深入了解第一个方向');
    const afterChild = await session();
    const child = afterChild.cards.at(-1);
    assert.deepEqual(child.parent_ids, [first.id]);

    const downward = await step('swipe-down-parent', async () => {
      await swipe(0, 110);
      await page.waitForFunction(question => document.querySelector('#answer-card')?.textContent.includes(question), first.question);
    });
    if (!downward) await focusByGraph(first.id);
    await focusByGraph(child.id);

    const leftward = await step('swipe-left-branch', async () => {
      await swipe(-110, 0);
      await page.waitForSelector('.compose-card');
      assert.match(await page.locator('.compose-card .depth-label').innerText(), /换个方向/);
    });
    if (!leftward) await page.locator('[data-action="compose"][data-value="branch"]').click();
    await send('探索另一个平行方向');
    const branched = await session();
    const sibling = branched.cards.at(-1);
    assert.deepEqual(sibling.parent_ids, child.parent_ids);
    await step('swipe-right-sibling', async () => {
      await swipe(110, 0);
      await page.waitForFunction(question => document.querySelector('#answer-card')?.textContent.includes(question), child.question);
    });

    const pinched = await step('pinch-overview', async () => {
      await page.locator('#answer-card h1').scrollIntoViewIfNeeded();
      const box = await page.locator('#answer-card h1').boundingBox();
      const y = box.y + box.height / 2;
      await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 145, y }, { x: 245, y }] });
      await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: 110, y }, { x: 280, y }] });
      await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
      await page.waitForSelector('#graph-scroll');
      // Allow the native pinch animation to settle before checking viewport scale.
      await page.waitForTimeout(300);
      const scale = await page.evaluate(() => visualViewport.scale);
      assert.equal(scale, 1, `Overview pinch also zoomed the page to ${scale}`);
      await noOverflow();
    });
    if (!pinched) {
      await touch.send('Emulation.setPageScaleFactor', { pageScaleFactor: 1 });
      await page.evaluate(() => scrollTo(0, 0));
      await page.locator('.view-switch [data-value="overview"]').click();
    }
    await step('overview-current-card-visible', async () => {
      const area = await page.locator('#graph-scroll').boundingBox();
      const focused = await page.locator('.graph-node.current').boundingBox();
      assert.ok(focused.x >= area.x && focused.x + focused.width <= area.x + area.width + 1, `Current card is clipped: ${JSON.stringify({ area, focused })}`);
    });
    await step('overview-scroll-and-selection', async () => {
      await page.locator(`[data-action="select"][data-id="${child.id}"]`).check();
      await page.locator(`[data-action="select"][data-id="${sibling.id}"]`).check();
      assert.equal(await page.locator('[data-action="merge"]').isEnabled(), true);
      await page.locator('[data-action="clear-selection"]').click();
      assert.equal(await page.locator('[data-action="merge"]').isEnabled(), false);
      await page.locator('[data-action="center-graph"]').click();
      await noOverflow();
    });

    await focusByGraph(sibling.id);
    await page.locator('[data-action="compose"][data-value="deepen"]').click();
    await send('[long] 请详细展开供手机滚动阅读');
    await step('long-answer-scroll', async () => {
      const answer = page.locator('#answer-content');
      const sizes = await answer.evaluate(element => ({ content: element.scrollHeight, viewport: element.clientHeight }));
      assert.ok(sizes.content > sizes.viewport + 20, `Expected scrollable answer ${JSON.stringify(sizes)}`);
      const before = (await session()).focus_id;
      await swipe(0, -110, '#answer-content');
      const scrollTop = await answer.evaluate(element => element.scrollTop);
      assert.ok(scrollTop > 0, `Answer did not scroll: ${scrollTop}`);
      assert.equal(await page.locator('.compose-card').count(), 0, 'Reading swipe opened a composer');
      assert.equal((await session()).focus_id, before, 'Reading scroll changed card');
    });
    await step('menu-help-and-preferences', async () => {
      await page.locator('[data-action="help"]').click();
      await page.waitForSelector('#dialog[open]');
      results.push({ name: 'mobile / help-dialog-390', status: 'passed', screenshot: await capture('help-dialog-390') });
      await page.keyboard.press('Escape');
      await page.locator('.mobile-menu').click();
      await page.locator('[data-action="settings"]').click();
      await page.waitForSelector('#preferences-form');
      assert.equal(await page.locator('#sidebar.open').count(), 0);
      results.push({ name: 'mobile / preferences-dialog-390', status: 'passed', screenshot: await capture('preferences-dialog-390') });
      await page.locator('#tone').fill('手机测试偏好');
      await page.locator('#preferences-form button[type="submit"]').click();
      await page.waitForFunction(() => !document.querySelector('#dialog').open);
    });
    await step('reduced-height-composer', async () => {
      await page.locator('[data-action="compose"][data-value="deepen"]').click();
      await page.setViewportSize({ width: 390, height: 500 });
      await page.locator('#question').fill('较小可视区域仍可填写问题');
      await page.locator('#question').focus();
      await page.locator('#chat-form button[type="submit"]').scrollIntoViewIfNeeded();
      assert.ok(await page.locator('#chat-form button[type="submit"]').isVisible());
      await noOverflow();
    });
    await step('narrow-320', async () => {
      await page.setViewportSize({ width: 320, height: 740 });
      await noOverflow();
      results.push({ name: 'mobile / narrow-composer-320', status: 'passed', screenshot: await capture('narrow-composer-320') });
      await page.locator('[data-action="cancel-compose"]').click();
      await noOverflow();
    });
    await step('landscape-844', async () => {
      await page.setViewportSize({ width: 844, height: 390 });
      await page.locator('.view-switch [data-value="overview"]').click();
      await noOverflow();
      await page.locator('.view-switch [data-value="focus"]').click();
      assert.ok(await page.locator('#answer-card').isVisible());
      const menu = page.locator('.mobile-menu');
      if (await menu.isVisible()) await menu.click();
      await page.locator('[data-action="settings"]').click();
      await page.waitForSelector('#preferences-form');
      await page.locator('[data-action="close-dialog"]').click();
    });
    await step('reduced-motion', async () => {
      assert.equal(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches), true);
      assert.equal(await page.locator('#answer-card').evaluate(element => getComputedStyle(element).transitionDuration), '0s');
      assert.deepEqual(errors, [], `Browser errors: ${errors.join('; ')}`);
    });
  } catch (error) {
    results.push({ name: 'mobile / workflow', status: 'failed', error: error.stack, screenshot: await capture('workflow-error') });
  } finally {
    await context.close();
  }
  return results;
}
