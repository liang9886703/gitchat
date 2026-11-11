import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const timeout = 12000;
const unique = () => `${Date.now()}-${Math.random().toString(16).slice(2)}`;

async function settled(page, status = '已完成') {
  await page.waitForFunction(expected => {
    const card = document.querySelector('#answer-card');
    return card && !document.querySelector('[data-action="stop"]') && card.querySelector('.card-bottom')?.textContent.includes(expected);
  }, status, { timeout });
}
async function submit(page, question) {
  await page.locator('#question').fill(question);
  await page.locator('#chat-form button[type="submit"]').click();
}
async function answer(page, question) {
  await submit(page, question);
  await settled(page);
}
async function session(page, baseURL) {
  const sessions = await (await page.request.get(`${baseURL}/api/sessions`)).json();
  assert.equal(sessions.length, 1, 'scenario has one conversation');
  return (await page.request.get(`${baseURL}/api/sessions/${sessions[0].id}`)).json();
}
async function lastCard(page, baseURL) {
  return (await session(page, baseURL)).cards.at(-1);
}
async function quoteAnswer(page) {
  await page.locator('#answer-content p').first().evaluate(element => {
    const range = document.createRange();
    range.selectNodeContents(element);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event('selectionchange'));
  });
  await page.locator('[data-action="quote"]').click();
  assert.ok((await page.locator('.quote').innerText()).length > 5, 'selected text is visible in composer');
}
async function stop(page) {
  await page.waitForSelector('#answer-card:not([data-card-id="pending"])');
  await page.locator('[data-action="stop"]').click();
  await settled(page, '已停止');
}

export async function run({ browser, baseURL, outputDir }) {
  await mkdir(outputDir, { recursive: true });
  const results = [];
  async function scenario(name, work) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(timeout);
    page.setDefaultNavigationTimeout(timeout);
    const consoleErrors = [];
    page.on('pageerror', error => consoleErrors.push(error.message));
    const screenshot = path.join(outputDir, `recovery-${name}.png`);
    try {
      await page.goto(baseURL);
      await page.waitForSelector('#question');
      const capture = async phase => {
        const checkpoint = path.join(outputDir, `recovery-${name}-${phase}.png`);
        await page.screenshot({ path: checkpoint, fullPage: true });
        results.push({ name: `recovery/${name}/${phase}`, status: 'passed', screenshot: checkpoint });
      };
      await work(page, context, capture);
      assert.deepEqual(consoleErrors, [], 'browser has no uncaught JavaScript errors');
      await page.screenshot({ path: screenshot, fullPage: true });
      results.push({ name: `recovery/${name}`, status: 'passed', screenshot });
    } catch (error) {
      await page.screenshot({ path: screenshot, fullPage: true }).catch(() => {});
      results.push({ name: `recovery/${name}`, status: 'failed', screenshot, error: error.message, consoleErrors });
    } finally {
      await context.close();
    }
  }

  await scenario('initial-configuration-error-reconnects', async (page, context, capture) => {
    let failed = false;
    await page.route('**/api/config', async route => {
      if (!failed) {
        failed = true;
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: '测试中的配置服务暂时不可用' }) });
      } else await route.continue();
    });
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.locator('#main .empty').waitFor({ state: 'visible' });
    assert.ok((await page.locator('#main').innerText()).includes('配置服务暂时不可用'));
    await capture('visible-reconnect');
    await page.getByRole('button', { name: '重新连接', exact: true }).click();
    await page.waitForSelector('#question');
    await answer(page, `重新连接后正常探索 ${unique()}`);
  });

  await scenario('invalid-model-output-can-start-again', async (page, context, capture) => {
    await submit(page, `[invalid] 检查模型结构校验 ${unique()}`);
    await settled(page, '生成失败');
    const failed = await lastCard(page, baseURL);
    assert.equal(failed.status, 'error');
    assert.equal(failed.answer, '', 'malformed model JSON is never published as an answer');
    assert.ok((await page.locator('.error-box').innerText()).includes('JSON'));
    await capture('visible-validation-error');
    await page.locator('[data-action="new"]').first().click();
    await answer(page, `校验失败之后正常探索 ${unique()}`);
  });

  await scenario('stream-read-failure-cancels-live-generation', async (page, context, capture) => {
    await page.evaluate(() => {
      const originalFetch = window.fetch.bind(window);
      let inject = true;
      window.fetch = async (input, options) => {
        const response = await originalFetch(input, options);
        if (!inject || !String(input).endsWith('/chat') || !response.ok) return response;
        inject = false;
        const upstream = response.body.getReader();
        const body = new ReadableStream({
          async start(controller) {
            const first = await upstream.read();
            if (first.value) controller.enqueue(first.value);
            // Forward the server's real created event before failing the browser read.
            // The application must abort its fetch to release the live upstream request.
            setTimeout(() => controller.error(new TypeError('Injected interrupted response stream')), 150);
          },
          cancel(reason) { return upstream.cancel(reason); },
        });
        return new Response(body, { status: response.status, headers: response.headers });
      };
    });
    await submit(page, `[slow] 流读取中断时的生成 ${unique()}`);
    await settled(page, '已停止');
    assert.equal((await lastCard(page, baseURL)).status, 'cancelled');
    await capture('visible-disconnect');
    await page.locator('[data-action="retry"]').click();
    await settled(page);
  });

  await scenario('temporary-provider-error-and-quoted-retry', async (page, context, capture) => {
    await answer(page, `原始背景 ${unique()}`);
    await quoteAnswer(page);
    const question = `[once] 结合所选内容进一步分析 ${unique()}`;
    await submit(page, question);
    await settled(page, '生成失败');
    const failed = await lastCard(page, baseURL);
    assert.ok(failed.quote, 'failed card persists the selected quote');
    await capture('visible-provider-error');
    await page.locator('[data-action="retry"]').click();
    await settled(page);
    const retried = await lastCard(page, baseURL);
    assert.equal(retried.question, question);
    assert.equal(retried.quote, failed.quote, 'retry keeps the original quoted text');
    assert.deepEqual(retried.parent_ids, failed.parent_ids, 'retry keeps the same context');
  });

  await scenario('stop-and-retry-keeps-quote', async (page, context, capture) => {
    await answer(page, `取消测试背景 ${unique()}`);
    await quoteAnswer(page);
    const question = `[slow] 保留引文后深入 ${unique()}`;
    await submit(page, question);
    await stop(page);
    const cancelled = await lastCard(page, baseURL);
    assert.equal(cancelled.status, 'cancelled');
    assert.ok(cancelled.quote);
    await capture('stopped');
    await page.locator('[data-action="retry"]').click();
    await settled(page);
    const retried = await lastCard(page, baseURL);
    assert.equal(retried.question, question);
    assert.equal(retried.quote, cancelled.quote);
    assert.deepEqual(retried.parent_ids, cancelled.parent_ids);
  });

  await scenario('merge-stop-and-retry-keeps-parents', async (page, context, capture) => {
    await answer(page, `合并方案一 ${unique()}`);
    await page.locator('[data-action="compose"][data-value="branch"]').click();
    await answer(page, `合并方案二 ${unique()}`);
    await page.locator('.view-switch [data-value="overview"]').click();
    await page.locator('input[data-action="select"]').nth(0).check();
    await page.locator('input[data-action="select"]').nth(1).check();
    await page.locator('[data-action="merge"]').click();
    await submit(page, `[slow] 合并并比较 ${unique()}`);
    await stop(page);
    const cancelled = await lastCard(page, baseURL);
    assert.equal(cancelled.parent_ids.length, 2);
    assert.equal(cancelled.operation, 'merge');
    await capture('stopped-merge');
    await page.locator('[data-action="retry"]').click();
    await settled(page);
    const retried = await lastCard(page, baseURL);
    assert.equal(retried.operation, 'merge');
    assert.deepEqual(retried.parent_ids, cancelled.parent_ids);
  });

  await scenario('persistent-provider-error-can-start-again', async (page, context, capture) => {
    await submit(page, `[fail] 服务故障 ${unique()}`);
    await settled(page, '生成失败');
    assert.ok((await page.locator('.error-box').innerText()).length > 10);
    await capture('visible-provider-error');
    await page.locator('[data-action="new"]').first().click();
    await answer(page, `故障之后正常探索 ${unique()}`);
    assert.ok((await page.locator('#answer-content').innerText()).length > 10);
  });

  await scenario('reload-during-generation-recovers', async (page, context, capture) => {
    await submit(page, `[slow] 刷新期间的生成 ${unique()}`);
    await page.waitForSelector('#answer-card:not([data-card-id="pending"])');
    await page.reload({ waitUntil: 'domcontentloaded' });
    await settled(page, '已停止');
    const interrupted = await lastCard(page, baseURL);
    assert.equal(interrupted.status, 'cancelled');
    await capture('reloaded-interruption');
    await page.locator('[data-action="retry"]').click();
    await settled(page);
  });

  await scenario('reload-before-session-created-keeps-question', async page => {
    let release;
    const gate = new Promise(resolve => {release=resolve;});
    await page.route('**/api/sessions', async route => {
      if(route.request().method()==='POST') { await gate; await route.abort().catch(()=>{}); }
      else await route.continue();
    });
    const question=`发送建立前保留的问题 ${unique()}`;
    await submit(page,question);
    await page.locator('#answer-card[data-card-id="pending"]').waitFor();
    await page.reload({waitUntil:'domcontentloaded'});
    release();
    await page.locator('#question').waitFor();
    assert.equal(await page.locator('#question').inputValue(),question);
    assert.equal(await page.locator('#answer-card').count(),0);
  });

  await scenario('session-creation-failure-retains-draft', async page => {
    let failed = false;
    await page.route('**/api/sessions', async route => {
      if (route.request().method() === 'POST' && !failed) {
        failed = true;
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: '测试中的临时不可用' }) });
      } else await route.continue();
    });
    const question = `创建失败后的原始草稿 ${unique()}`;
    await submit(page, question);
    await page.waitForFunction(() => !document.querySelector('[data-action="stop"]') && document.querySelector('#question') && !document.querySelector('#question').disabled, null, { timeout });
    assert.equal(await page.locator('#question').inputValue(), question);
    assert.ok((await page.locator('#toast').innerText()).includes('临时不可用'));
    await page.locator('#chat-form button[type="submit"]').click();
    await settled(page);
  });

  await scenario('chat-http-error-retains-draft', async page => {
    await answer(page, `发送故障背景 ${unique()}`);
    await page.locator('[data-action="compose"][data-value="deepen"]').click();
    let failed = false;
    await page.route('**/api/sessions/*/chat', async route => {
      if (!failed) {
        failed = true;
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: '测试中的聊天服务暂时不可用' }) });
      } else await route.continue();
    });
    const question = `需要保留的发送草稿 ${unique()}`;
    await submit(page, question);
    await page.waitForFunction(() => !document.querySelector('[data-action="stop"]'), null, { timeout });
    await page.locator('#question').waitFor({ state: 'visible' });
    assert.equal(await page.locator('#question').inputValue(), question);
    await page.locator('#chat-form button[type="submit"]').click();
    await settled(page);
  });

  await scenario('malformed-sse-recovers-saved-card', async page => {
    await page.route('**/api/sessions/*/chat', async route => {
      const response = await route.fetch();
      await route.fulfill({ response, body: 'event: message\ndata: {malformed-json}\n\n', headers: { ...response.headers(), 'content-type': 'text/event-stream' } });
    });
    await submit(page, `流格式异常但已落盘 ${unique()}`);
    await settled(page);
    assert.equal((await lastCard(page, baseURL)).status, 'completed');
    await page.unroute('**/api/sessions/*/chat');
    await page.locator('[data-action="compose"][data-value="deepen"]').click();
    await answer(page, `异常后的下一轮 ${unique()}`);
  });

  await scenario('invalid-import-and-share-isolation', async (page, context, capture) => {
    await page.locator('[data-action="import"]').first().click();
    await page.locator('#import-token').fill(`missing-${unique()}`);
    await page.locator('#import-form button[type="submit"]').click();
    await page.locator('#dialog-status').waitFor({ state: 'visible' });
    assert.ok((await page.locator('#dialog-status').innerText()).includes('resource not found'));
    assert.ok(await page.locator('#dialog').evaluate(dialog => dialog.open));
    await capture('visible-import-error');
    await page.locator('[data-action="close-dialog"]').click();
    await answer(page, `我的私人探索 ${unique()}`);
    const original = await session(page, baseURL);
    await page.locator('[data-action="card-menu"]').click();
    await page.locator('[data-action="share"]').click();
    const shareURL = await page.locator('#share-url').inputValue();
    const stranger = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    try {
      const other = await stranger.newPage();
      other.setDefaultTimeout(timeout);
      await other.goto(baseURL);
      const privateResponse = await other.request.get(`${baseURL}/api/sessions/${original.id}`);
      assert.equal(privateResponse.status(), 404);
      await other.goto(shareURL);
      await other.waitForSelector('.shared-banner');
      assert.equal(await other.locator('[data-action="favorite"]').isDisabled(), true);
      await other.locator('[data-action="import-current"]').first().click();
      await other.waitForFunction(() => !document.querySelector('.shared-banner'), null, { timeout });
      const imported = await session(other, baseURL);
      assert.notEqual(imported.id, original.id);
      assert.equal(imported.cards.length, original.cards.length);
      await other.locator('[data-action="compose"][data-value="deepen"]').click();
      await answer(other, `分享导入后独立继续 ${unique()}`);
      const unchanged = await (await context.request.get(`${baseURL}/api/sessions/${original.id}`)).json();
      assert.equal(unchanged.cards.length, original.cards.length);
    } finally { await stranger.close(); }
    await page.locator('[data-action="close-dialog"]').click();
  });

  await scenario('missing-shared-link-can-return-home', async page => {
    await page.goto(`${baseURL}/?share=missing-${unique()}`);
    await page.waitForSelector('#main .empty');
    assert.ok((await page.locator('#main').innerText()).includes('resource not found'));
    await page.locator('.brand').click();
    await page.waitForSelector('#question');
    await answer(page, `失效链接后新建 ${unique()}`);
  });
  return results;
}
