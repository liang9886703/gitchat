import assert from 'node:assert/strict';
import { ask, idle, session, screenshot, noOverflow } from './helpers.mjs';

export async function run({ browser, baseURL, outputDir }) {
  const results = [];
  async function scenario(name, work, mobile = false) {
    const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, reducedMotion: 'reduce', hasTouch: mobile, isMobile: mobile });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.setDefaultTimeout(15000);
    try {
      await page.goto(baseURL);
      await page.waitForSelector('#question');
      await work(page);
      await noOverflow(page);
      assert.deepEqual(errors, []);
      results.push({ name: `experience/${name}`, status: 'passed', screenshot: await screenshot(page, outputDir, `experience-${name}`) });
    } catch (error) {
      results.push({ name: `experience/${name}`, status: 'failed', error: error.stack, screenshot: await screenshot(page, outputDir, `experience-${name}`).catch(() => '') });
    } finally { await context.close(); }
  }
  const compose = (page, operation = 'deepen') => page.locator(`[data-action="compose"][data-value="${operation}"]`).click();
  const overview = page => page.locator('.view-switch [data-value="overview"]').click();

  async function graphEdgesMatchNodes(page, cards, reason) {
    const geometry = await page.evaluate(cards => {
      const svg = document.querySelector('#graph-canvas');
      const nodes = new Map([...document.querySelectorAll('[data-node]')].map(node => [node.dataset.node, node]));
      const edges = [];
      const center = (element, side) => { const box = element.getBoundingClientRect(); return { x: box.left + box.width / 2, y: side === 'top' ? box.top : box.bottom }; };
      for (const card of cards) {
        const child = nodes.get(card.id);
        if (!child) continue;
        for (const id of card.parent_ids || []) if (nodes.has(id)) edges.push({ from: center(nodes.get(id), 'bottom'), to: center(child, 'top'), predicted: false });
      }
      for (const ghost of document.querySelectorAll('.graph-node[data-parent]')) {
        const parent = nodes.get(ghost.dataset.parent);
        if (parent) edges.push({ from: center(parent, 'bottom'), to: center(ghost, 'top'), predicted: true });
      }
      const paths = [...svg.querySelectorAll('path')].map(path => {
        const matrix = path.getScreenCTM();
        const from = path.getPointAtLength(0).matrixTransform(matrix);
        const to = path.getPointAtLength(path.getTotalLength()).matrixTransform(matrix);
        return { from: { x: from.x, y: from.y }, to: { x: to.x, y: to.y }, predicted: path.hasAttribute('stroke-dasharray') };
      });
      return { edges, paths };
    }, cards);
    assert.ok(geometry.edges.length > 0, `No graph connections to verify: ${reason}`);
    assert.equal(geometry.paths.length, geometry.edges.length, `Every visible relationship needs one connector: ${reason}`);
    for (const path of geometry.paths) {
      assert.ok(geometry.edges.some(edge => edge.predicted === path.predicted && Math.abs(edge.from.x - path.from.x) < 1.5 && Math.abs(edge.from.y - path.from.y) < 1.5 && Math.abs(edge.to.x - path.to.x) < 1.5 && Math.abs(edge.to.y - path.to.y) < 1.5), `Detached graph connector after ${reason}: ${JSON.stringify({ path, edges: geometry.edges })}`);
    }
  }

  await scenario('branch-context-and-editable-suggestion', async page => {
    await ask(page, '旅行规划总背景');
    const first = (await session(page)).cards.at(-1);
    await compose(page, 'branch');
    assert.equal(await page.locator('.compose-card').getAttribute('data-depth'), '1');
    assert.equal(await page.locator('.context-chip').count(), 0);
    assert.match(await page.locator('.context-empty').innerText(), /独立方向/);
    await page.keyboard.press('Escape');
    await compose(page);
    await ask(page, '只探索美食路线');
    const child = (await session(page)).cards.at(-1);
    await compose(page, 'branch');
    assert.equal(await page.locator('.compose-card').getAttribute('data-depth'), String(child.depth));
    assert.equal(await page.locator('.context-chip').innerText(), first.title);
    assert.equal(await page.locator('.context-chip').innerText().then(text => text.includes(child.title)), false);
    const count = (await session(page)).cards.length;
    await page.locator('.compose-card [data-action="suggestion"]').first().click();
    assert.ok((await page.locator('#question').inputValue()).length > 0);
    assert.equal(await page.locator('[data-action="operation"][data-value="branch"]').getAttribute('class'), 'active');
    assert.equal((await session(page)).cards.length, count, 'A suggested question must remain editable before sending');
    await page.locator('#question').fill('改为文化景点');
    await page.locator('#question').press('Control+Enter');
    await idle(page);
    const branched = (await session(page)).cards.at(-1);
    assert.deepEqual(branched.parent_ids, child.parent_ids);
    assert.equal(branched.depth, child.depth);
    assert.equal(branched.operation, 'branch');
  });

  await scenario('merge-depth-and-removable-sources', async page => {
    await ask(page, '比较学习路径');
    const first = (await session(page)).cards.at(-1);
    await compose(page); await ask(page, '详细探索其中一条');
    const child = (await session(page)).cards.at(-1);
    await overview(page);
    await page.locator(`[data-action="select"][data-id="${first.id}"]`).check();
    await page.locator(`[data-action="select"][data-id="${child.id}"]`).check();
    assert.equal(await page.locator('.source-chip').count(), 2);
    await page.locator('[data-action="merge"]').click();
    assert.equal(await page.locator('.compose-card').getAttribute('data-depth'), String(child.depth + 1));
    await page.locator(`.context-chip [data-action="remove-source"][data-id="${first.id}"]`).click();
    assert.equal(await page.locator('.context-chip').count(), 1);
    assert.equal(await page.locator('#chat-form button[type="submit"]').isDisabled(), true);
    assert.match(await page.locator('.compose-guidance').innerText(), /至少保留两张/);
    await page.keyboard.press('Escape');
    await page.waitForSelector('#answer-card');
  });

  await scenario('focused-overview-paging-preserves-selection', async page => {
    await ask(page, '方向 1');
    const initial = await session(page);
    await page.evaluate(async ({ sessionID, root }) => {
      for (let index = 2; index <= 9; index++) {
        const response = await fetch(`/api/sessions/${sessionID}/chat`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ operation: 'deepen', parent_ids: [root], question: `方向 ${index}`, request_id: crypto.randomUUID() }) });
        if (!response.ok) throw new Error(await response.text());
        await response.text();
      }
    }, { sessionID: initial.id, root: initial.root_id });
    await page.reload(); await page.waitForSelector('#answer-card');
    const saved = await session(page);
    const first = saved.cards[1]; const last = saved.cards.at(-1);
    await overview(page);
    assert.equal(await page.locator('[data-level="1"] .graph-node').count(), 3, 'Wide layers should start with current and adjacent cards');
    await page.locator(`[data-action="select"][data-id="${last.id}"]`).check();
    for (let i = 0; i < 7; i++) await page.locator('[data-action="graph-page"][aria-label="第 1 层上一条思路"]').click();
    await page.locator(`[data-action="select"][data-id="${first.id}"]`).check();
    assert.equal(await page.locator('.source-chip').count(), 2, 'Selected sources must survive paging out of view');
    assert.equal(await page.locator('[data-action="merge"]').isEnabled(), true);
    await page.locator('[data-action="graph-scope"]').click();
    assert.equal(await page.locator('[data-level="1"] .graph-node').count(), 9);
    assert.equal(await page.locator('[data-action="select"]:checked').count(), 2);
    await page.locator('[data-action="graph-scope"]').click();
    await page.locator('[data-action="center-graph"]').click();
    await page.waitForFunction(() => {
      const node = document.querySelector('.graph-node.current')?.getBoundingClientRect();
      const lane = document.querySelector('.graph-node.current')?.closest('.graph-lane').getBoundingClientRect();
      return node && lane && node.left >= lane.left - 1 && node.right <= lane.right + 1;
    });
    assert.equal(await page.locator('[data-level="1"] + .prediction-level').count(), 1, 'Predictions belong directly below the current card layer');
    await graphEdgesMatchNodes(page, saved.cards, 'centering current card');
    await page.locator('[data-action="graph-page"][aria-label="第 1 层上一条思路"]').click();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await graphEdgesMatchNodes(page, saved.cards, 'paging a layer');
    await page.locator('[data-level="1"] .graph-lane').evaluate(lane => { lane.scrollLeft += 94; });
    await page.locator('.prediction-level .graph-lane').evaluate(lane => { lane.scrollLeft += 81; });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await graphEdgesMatchNodes(page, saved.cards, 'scrolling source and prediction layers independently');
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await graphEdgesMatchNodes(page, saved.cards, 'resizing the window');
    await page.locator('[data-action="center-graph"]').click();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await graphEdgesMatchNodes(page, saved.cards, 'restoring current card after resize');
  });

  await scenario('merge-selection-is-bounded-to-sixteen', async page => {
    await ask(page, '可汇总的方向 1');
    const initial = await session(page);
    await page.evaluate(async ({ sessionID, root }) => {
      for (let index = 2; index <= 17; index++) {
        const response = await fetch(`/api/sessions/${sessionID}/chat`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ operation: 'deepen', parent_ids: [root], question: `可汇总的方向 ${index}`, request_id: crypto.randomUUID() }) });
        if (!response.ok) throw new Error(await response.text());
        await response.text();
      }
    }, { sessionID: initial.id, root: initial.root_id });
    await page.reload(); await page.waitForSelector('#answer-card');
    await overview(page); await page.locator('[data-action="graph-scope"]').click();
    const selectors = await page.locator('[data-action="select"]').evaluateAll(elements => elements.map(element => element.dataset.id));
    for (const id of selectors.slice(0, 16)) await page.locator(`[data-action="select"][data-id="${id}"]`).check();
    await page.locator(`[data-action="select"][data-id="${selectors[16]}"]`).click();
    assert.equal(await page.locator('[data-action="select"]:checked').count(), 16);
    assert.match(await page.locator('#toast').innerText(), /最多合并 16/);
    assert.equal(await page.locator('.source-chip').count(), 16);
    await page.locator('.source-chip [data-action="remove-source"]').first().click();
    await page.locator(`[data-action="select"][data-id="${selectors[16]}"]`).check();
    assert.equal(await page.locator('[data-action="select"]:checked').count(), 16);
  });

  await scenario('pending-card-and-stream-preserve-reading', async page => {
    await ask(page, '先建立背景');
    await compose(page);
    await page.evaluate(() => {
      const original = window.fetch;
      window.fetch = async (url, options) => {
        const response = await original(url, options);
        if (!String(url).endsWith('/chat') || !response.ok) return response;
        const text = await response.text();
        const frames = text.split('\n\n').filter(Boolean);
        const encoder = new TextEncoder();
        let index = 0;
        return new Response(new ReadableStream({
          async pull(controller) {
            await new Promise(resolve => setTimeout(resolve, index === 0 ? 300 : 35));
            if (index < frames.length) controller.enqueue(encoder.encode(frames[index++] + '\n\n'));
            else controller.close();
          },
        }), { status: response.status, headers: response.headers });
      };
    });
    await page.locator('#question').fill('深入分析 [long] 并保持阅读位置');
    await page.locator('#chat-form button[type="submit"]').click();
    await page.waitForSelector('#answer-card[data-card-id="pending"]');
    assert.match(await page.locator('#answer-card h1').innerText(), /深入分析/);
    await page.evaluate(() => { window.__pendingCard = document.querySelector('#answer-card'); });
    await page.waitForFunction(() => document.querySelector('#answer-card')?.dataset.cardId !== 'pending' && document.querySelector('#answer-content')?.textContent.length > 250);
    assert.equal(await page.evaluate(() => window.__pendingCard === document.querySelector('#answer-card')), true, 'Question and answer use the same card element');
    await page.evaluate(() => {
      const answer = document.querySelector('#answer-content');
      const text = answer.querySelector('p').firstChild;
      const range = document.createRange(); range.setStart(text, 0); range.setEnd(text, Math.min(8, text.textContent.length));
      getSelection().removeAllRanges(); getSelection().addRange(range);
      window.__selectedText = getSelection().toString();
      window.__answerElement = answer;
      window.__answerLength = answer.textContent.length;
    });
    await page.waitForFunction(() => document.querySelector('#answer-content').textContent.length > window.__answerLength + 80);
    assert.equal(await page.evaluate(() => window.__answerElement === document.querySelector('#answer-content')), true);
    assert.equal(await page.evaluate(() => getSelection().toString() === window.__selectedText), true, 'Growing answer must preserve selected text');
    await idle(page);
    assert.equal(await page.evaluate(() => window.__pendingCard === document.querySelector('#answer-card')), true);
  });

  await scenario('mobile-keyboard-newline-and-touch-controls', async page => {
    await page.locator('#question').fill('第一行');
    await page.locator('#question').press('Enter');
    await page.locator('#question').pressSequentially('second line');
    assert.equal(await page.locator('#question').inputValue(), '第一行\nsecond line');
    assert.equal(await page.locator('#answer-card').count(), 0);
    assert.ok(await page.locator('#question').evaluate(element => parseFloat(getComputedStyle(element).fontSize) >= 16));
    await page.locator('#chat-form button[type="submit"]').click(); await idle(page);
    const controls = await page.locator('#answer-card button, .nav-actions button, .continue-prompt').evaluateAll(elements => elements.map(element => ({ label: element.textContent, width: element.getBoundingClientRect().width, height: element.getBoundingClientRect().height })));
    for (const control of controls) assert.ok(control.width >= 44 && control.height >= 44, `Small touch target ${JSON.stringify(control)}`);
    await page.setViewportSize({ width: 320, height: 640 }); await noOverflow(page);
    await compose(page);
    await page.evaluate(() => { document.documentElement.classList.add('keyboard-open'); document.documentElement.style.setProperty('--viewport-height', '380px'); });
    await page.setViewportSize({ width: 320, height: 380 });
    await page.locator('#question').fill('键盘打开时仍能提问');
    await page.locator('#chat-form button[type="submit"]').click(); await idle(page);
    assert.equal((await session(page)).cards.at(-1).question, '键盘打开时仍能提问');
  }, true);

  return results;
}
