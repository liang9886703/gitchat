import assert from 'node:assert/strict';
import { ask, session, screenshot, noOverflow } from './helpers.mjs';
import { networkGateway } from './network-gateway.mjs';

export async function run({browser,baseURL,outputDir}) {
  const gateway=await networkGateway(baseURL);
  baseURL=gateway.baseURL;
  const results = [];
  const contexts = [];
  const errors = [];
  const context = async options => {
    const value = await browser.newContext(options);
    contexts.push(value);
    const page = await value.newPage();
    page.setDefaultTimeout(12000);
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(baseURL);
    await page.locator('#question').waitFor();
    return page;
  };
  const check = async (name,page,fn) => {
    console.log(`  platform/${name}`);
    try { await fn(); await noOverflow(page); results.push({name:`platform/${name}`,status:'passed',screenshot:await screenshot(page,outputDir,`platform-${name}`)}); }
    catch(error) { results.push({name:`platform/${name}`,status:'failed',error:error.stack,screenshot:await screenshot(page,outputDir,`platform-${name}-failed`).catch(()=>undefined)}); }
  };
  try {
    const desktop = await context({viewport:{width:1440,height:1000}});
    await check('install-manifest-and-icons',desktop,async()=> {
      const manifest = await desktop.evaluate(async()=> (await fetch('/manifest.webmanifest')).json());
      assert.equal(manifest.display,'standalone');
      assert.equal(manifest.scope,'/');
      assert.equal(manifest.icons.length,3);
      for (const item of manifest.icons) {
        const size = await desktop.evaluate(async src=> {const image=new Image(); image.src=src; await image.decode(); return `${image.width}x${image.height}`;},item.src);
        assert.equal(size,item.sizes);
      }
      await desktop.locator('[data-platform="install"]').click();
      for (const os of ['android','ios','mac','windows']) {
        await desktop.locator(`[data-os="${os}"]`).click();
        assert.equal(await desktop.locator(`[data-os="${os}"]`).getAttribute('aria-pressed'),'true');
        assert.ok((await desktop.locator('.platform-guide p').innerText()).length>20);
      }
    });
    await desktop.locator('[data-action="close-dialog"]').click();
    await check('install-prompt-cancel-is-recoverable',desktop,async()=> {
      await desktop.evaluate(()=> {const event=new Event('beforeinstallprompt');event.prompt=async()=>{};event.userChoice=Promise.resolve({outcome:'dismissed'});window.dispatchEvent(event);});
      await desktop.locator('[data-platform="install"]').click();
      await desktop.locator('[data-platform="install-now"]').click();
      await desktop.locator('.platform-guide').waitFor();
      assert.equal(await desktop.locator('[data-platform="install-now"]').count(),0);
    });
    await desktop.locator('[data-action="close-dialog"]').click();
    await ask(desktop,'跨设备探索：先考虑美食，再比较文化');
    const initial = await session(desktop);
    const phone = await context({viewport:{width:390,height:844},isMobile:true,hasTouch:true});
    await ask(phone,'手机原空间里尚未整理的灵感');
    const originalPhoneSession = await session(phone);
    const stale = await phone.context().newPage();
    stale.setDefaultTimeout(12000);
    await stale.goto(baseURL);
    await stale.locator('#answer-card').waitFor();
    await stale.locator('.mobile-menu').click();
    await stale.locator('[data-action="settings"]').click();
    await stale.locator('#tone').fill('不应写入新空间的旧偏好');
    const oldFingerprint = await stale.evaluate(async()=> (await(await fetch('/api/config')).json()).workspace_id);
    let code;
    await check('create-private-one-time-pair-code',desktop,async()=> {
      await desktop.locator('[data-platform="devices"]').click();
      await desktop.locator('[data-platform="create-pair"]').click();
      await desktop.locator('#device-pair-code').waitFor();
      code = await desktop.locator('#device-pair-code').inputValue();
      assert.ok(code.length>=32);
      assert.ok((await desktop.locator('#pair-result').innerText()).includes('使用后立即失效'));
    });
    await check('phone-connects-same-space',phone,async()=> {
      await phone.locator('.mobile-menu').click();
      await phone.locator('[data-platform="devices"]').click();
      await phone.locator('#device-code').fill(code);
      await phone.locator('#pair-confirm').check();
      await phone.locator('#device-pair-form button[type="submit"]').click();
      await phone.waitForFunction(()=>!document.querySelector('#dialog')?.open && document.querySelector('#sessions')?.textContent.includes('跨设备探索'));
      await phone.locator('.mobile-menu').click();
      await phone.locator(`#sessions [data-id="${initial.id}"]`).click();
      await phone.waitForFunction(id=>localStorage.getItem('gitchat.session')===id,initial.id);
      await phone.locator('#answer-card').waitFor();
      assert.equal((await session(phone)).id,initial.id);
      await phone.locator('[data-action="compose"][data-value="deepen"]').click();
      await ask(phone,'在手机上接着规划文化路线');
    });
    await check('desktop-refreshes-phone-progress',desktop,async()=> {
      await desktop.locator('[data-action="close-dialog"]').click();
      await desktop.locator('[data-platform="devices"]').click();
      await desktop.locator('[data-platform="refresh"]').click();
      await desktop.waitForFunction(()=>document.querySelector('#page-subtitle')?.textContent.includes('2 张卡片'));
      const updated = await session(desktop);
      assert.equal(updated.cards.filter(card=>card.id!==updated.root_id).length,2);
    });
    await check('pair-code-cannot-be-reused',phone,async()=> {
      const result = await phone.evaluate(async code=> {const response=await fetch('/api/devices/pair',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({code})});return response.status;},code);
      assert.ok([400,409].includes(result));
      assert.equal((await session(phone)).id,initial.id);
    });
    await check('old-tab-cannot-write-into-new-space',stale,async()=> {
      await stale.locator('[data-platform="workspace-reload"]').waitFor();
      assert.equal(await stale.locator('#preferences-form').count(),0);
      const result=await stale.evaluate(async workspace=> {
        const response=await fetch('/api/preferences',{method:'PUT',headers:{'Content-Type':'application/json','X-GitChat-Workspace':workspace},body:JSON.stringify({tone:'旧标签不应提交'})});
        return {status:response.status,preferences:await(await fetch('/api/preferences')).json()};
      },oldFingerprint);
      assert.equal(result.status,409);
      assert.notEqual(result.preferences.tone,'旧标签不应提交');
    });
    await check('return-to-original-phone-space',phone,async()=> {
      await phone.locator('.mobile-menu').click();
      await phone.locator('[data-platform="devices"]').click();
      await phone.locator('[data-platform="restore-space"]').waitFor();
      await phone.locator('[data-platform="restore-space"]').click();
      await phone.waitForFunction(()=>!document.querySelector('#dialog')?.open && document.querySelector('#sessions')?.textContent.includes('手机原空间'));
      await phone.locator('.mobile-menu').click();
      await phone.locator(`#sessions [data-id="${originalPhoneSession.id}"]`).click();
      await phone.waitForFunction(id=>localStorage.getItem('gitchat.session')===id,originalPhoneSession.id);
      assert.equal((await session(phone)).id,originalPhoneSession.id);
    });
    await desktop.locator('[data-action="close-dialog"]').click();
    await check('offline-shell-keeps-only-public-assets',desktop,async()=> {
      await desktop.waitForFunction(()=>navigator.serviceWorker?.controller,null,{timeout:15000});
      await desktop.evaluate(async()=> {
        await fetch('/api/sessions');
        const registration=await navigator.serviceWorker.ready;
        if (!registration.active) throw new Error('No active service worker');
      });
      const urls = await desktop.evaluate(async()=> {const urls=[];for (const name of await caches.keys()) {const cache=await caches.open(name);for(const request of await cache.keys())urls.push(new URL(request.url).pathname);}return urls;});
      assert.ok(urls.includes('/offline.html'));
      assert.equal(urls.some(url=>url.startsWith('/api/') || url==='/' || url.includes('share=')),false);
      gateway.setAvailable(false);
      await desktop.reload();
      await desktop.locator('#offline-note').waitFor();
      await desktop.locator('#offline-note').fill('离线灵感：这条路线为什么经过这里？');
      await desktop.reload();
      assert.equal(await desktop.locator('#offline-note').inputValue(),'离线灵感：这条路线为什么经过这里？');
    });
    gateway.setAvailable(true);
    await check('offline-inspiration-recovery-without-auto-send',desktop,async()=> {
      await desktop.locator('#offline-note-form button[type="submit"]').click();
      await desktop.locator('[data-platform="recover-note"]').waitFor();
      await desktop.locator('[data-platform="recover-note"]').click();
      await desktop.locator('[data-platform="use-note"]').click();
      assert.equal(await desktop.locator('#question').inputValue(),'离线灵感：这条路线为什么经过这里？');
      assert.equal(await desktop.locator('#answer-card').count(),0);
      assert.equal(await desktop.evaluate(()=>JSON.parse(JSON.stringify(localStorage.getItem('gitchat.unsent-inspiration')))),'离线灵感：这条路线为什么经过这里？');
    });
    await check('native-settings-preserves-unsent-question',desktop,async()=> {
      await desktop.evaluate(()=> {window.__GITCHAT_NATIVE__=true;window.dispatchEvent(new Event('gitchat:native-ready'));});
      assert.equal(await desktop.locator('[data-platform="install"]').isVisible(),false);
      await desktop.locator('[data-platform="native-settings"]').click();
      assert.ok((await desktop.locator('#dialog').innerText()).includes('先留住当前的问题'));
      await desktop.locator('[data-action="close-dialog"]').first().click();
      assert.ok(await desktop.locator('#question').inputValue());
    });
    const storageFailure=await context({viewport:{width:390,height:844},isMobile:true,hasTouch:true});
    await check('offline-storage-failure-keeps-unsent-note',storageFailure,async()=> {
      await storageFailure.goto(`${baseURL}/offline.html`);
      await storageFailure.evaluate(()=> {Storage.prototype.setItem=function(){throw new DOMException('quota','QuotaExceededError');};});
      await storageFailure.locator('#offline-note').fill('必须保留，不能因保存失败而消失');
      await storageFailure.locator('#offline-note-form button[type="submit"]').click();
      assert.ok(storageFailure.url().endsWith('/offline.html'));
      assert.equal(await storageFailure.locator('#offline-note').inputValue(),'必须保留，不能因保存失败而消失');
      assert.ok((await storageFailure.locator('#offline-save-status').innerText()).includes('无法保存'));
    });
    const updatePage=await context({viewport:{width:1100,height:800}});
    const writingTab=await updatePage.context().newPage();
    writingTab.setDefaultTimeout(12000);
    await writingTab.goto(baseURL);
    await writingTab.locator('#question').fill('另一个标签页里，还没有发送的思路');
    await check('updating-one-tab-preserves-other-tab-draft',writingTab,async()=> {
      await updatePage.waitForFunction(()=>navigator.serviceWorker?.controller);
      await writingTab.waitForFunction(()=>navigator.serviceWorker?.controller);
      await writingTab.evaluate(()=> {window.keepTabIdentity={};});
      await updatePage.evaluate(async()=> {await navigator.serviceWorker.register('/sw.js?revision=update-check',{updateViaCache:'none'});});
      await updatePage.locator('[data-platform="update"]').waitFor();
      await writingTab.locator('[data-platform="update"]').waitFor();
      await updatePage.locator('[data-platform="update"]').click();
      await updatePage.waitForFunction(()=>navigator.serviceWorker?.controller?.scriptURL.includes('revision=update-check'));
      await writingTab.waitForFunction(()=>navigator.serviceWorker?.controller?.scriptURL.includes('revision=update-check'));
      assert.equal(await writingTab.locator('#question').inputValue(),'另一个标签页里，还没有发送的思路');
      assert.equal(await writingTab.evaluate(()=>!!window.keepTabIdentity),true);
    });
    for (const [os,viewport,userAgent] of [
      ['android',{width:360,height:800},'Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/130.0.0.0 Mobile Safari/537.36'],
      ['ios',{width:375,height:812},'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'],
      ['mac',{width:1024,height:768},'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15'],
      ['windows',{width:1280,height:800},'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0'],
    ]) {
      const page=await context({viewport,userAgent,isMobile:['android','ios'].includes(os),hasTouch:['android','ios'].includes(os)});
      await check(`${os}-adaptive-install-and-compose`,page,async()=> {
        if (viewport.width<760) await page.locator('.mobile-menu').click();
        await page.locator('[data-platform="install"]').click();
        assert.equal(await page.locator(`[data-os="${os}"]`).getAttribute('aria-pressed'),'true');
        await page.locator('[data-action="close-dialog"]').click();
        await page.locator('#question').fill('不同设备上，沿自己的节奏探索');
        if (viewport.width<760) {
          await page.locator('#question').press('Enter');
          assert.equal(await page.locator('#answer-card').count(),0,'Phone Enter must not submit');
          assert.ok((await page.locator('#question').inputValue()).endsWith('\n'));
          assert.ok(await page.locator('#question').evaluate(el=>parseFloat(getComputedStyle(el).fontSize)>=16));
        }
        await page.locator('#chat-form button[type="submit"]').click();
        await page.locator('#answer-card').waitFor();
        await page.waitForFunction(()=>document.querySelector('#answer-card .card-bottom')?.textContent.includes('已完成'));
      });
    }
    assert.deepEqual(errors,[],'No uncaught frontend exceptions');
  } finally { for (const value of contexts) await value.close(); await gateway.close(); }
  return results;
}
