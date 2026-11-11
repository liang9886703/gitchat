import assert from 'node:assert/strict';
import {ask, session, screenshot, noOverflow} from './helpers.mjs';
export async function run({browser,baseURL,modelURL,outputDir}) {
  if(!modelURL)throw new Error('Model suite needs the local protocol fixture');
  const results=[],errors=[],contexts=[];
  const context=await browser.newContext({viewport:{width:1440,height:1050}});contexts.push(context);const page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
  const check=async(name,fn,p=page)=>{console.log('  models/'+name);try{await fn();await noOverflow(p);results.push({name:'models/'+name,status:'passed',screenshot:await screenshot(p,outputDir,'models-'+name)})}catch(e){results.push({name:'models/'+name,status:'failed',error:e.stack,screenshot:await screenshot(p,outputDir,'models-'+name+'-failed').catch(()=>undefined)});throw e}};
  const open=async()=>{await page.locator('#model-chip').click();await page.locator('#model-active').waitFor()};
  const close=async()=>{await page.locator('[data-action="close-dialog"]').click();await page.locator('#dialog').waitFor({state:'hidden'})};
  const add=async(protocol)=>{await page.locator('[data-model="add"]').click();await page.locator('#model-name').fill(protocol);await page.locator('#model-protocol').selectOption(protocol);await page.locator('#model-url').fill(modelURL+(protocol==='ollama'?'':protocol==='gemini'?'/v1beta':'/v1'));await page.locator('#model-key').fill('test-secret');};
  try {
    await page.goto(baseURL);await page.locator('#question').waitFor();await page.locator('#question').fill('保留尚未发送的思考');
    await check('visible-entry-and-empty-state',async()=>{assert.equal(await page.locator('[data-model="open"]').count(),2);await open();await page.locator('.model-empty').waitFor()});
    for(const protocol of ['openai-chat','openai-responses','anthropic','gemini','ollama']){
      await check(protocol+'-test-save-generate',async()=>{
        await add(protocol);await page.locator('[data-model="models"]').click();await page.getByText('模型列表已更新。',{exact:true}).waitFor();assert.ok(await page.locator('#model-id').inputValue());await page.locator('#model-id').fill('test-model');
        await page.locator('[data-model="test"]').click();await page.locator('#model-status').filter({hasText:'连接成功'}).waitFor();
        await page.locator('#model-form [type="submit"]').click();await page.locator('#model-active').waitFor();assert.equal(await page.locator('.model-badge').innerText(),'使用中');
        const view=await page.evaluate(async()=>(await fetch('/api/model-connections')).json());assert.ok(!JSON.stringify(view).includes('test-secret'));assert.ok(view.connections.at(-1).has_key);
        await close();if(protocol==='openai-chat')assert.equal(await page.locator('#question').inputValue(),'保留尚未发送的思考');
        if(!await page.locator('#question').count())await page.locator('[data-action="compose"][data-value="deepen"]').first().click();
        await ask(page,'测试 '+protocol+' 的卡片');const s=await session(page);assert.equal(s.cards.at(-1).status,'completed');assert.equal(s.cards.at(-1).model,'test-model');await open();
      });
    }
    await check('reload-and-secret-not-refilled',async()=>{await close();await page.reload();await page.locator('#answer-card').waitFor();await open();assert.equal(await page.locator('.model-item').count(),5);await page.locator('[data-model="edit"]').first().click();assert.equal(await page.locator('#model-key').inputValue(),'');assert.ok((await page.locator('#model-key').getAttribute('placeholder')).includes('保留'));});
    await check('auth-failure-keeps-form',async()=>{await page.locator('#model-key').fill('bad-key');await page.locator('[data-model="test"]').click();await page.locator('#model-status').filter({hasText:'401'}).waitFor();assert.equal(await page.locator('#model-key').inputValue(),'bad-key');assert.ok(!(await page.locator('#model-status').innerText()).includes('test-secret'));await page.locator('#model-key').fill('');});
    await check('edit-preserves-key-and-uses-prompt-mode',async()=>{await page.locator('#model-name').fill('日常思考');await page.locator('.model-advanced summary').click();await page.locator('#model-json').selectOption('prompt');await page.locator('#model-stream').uncheck();await page.locator('[data-model="test"]').click();await page.locator('#model-status').filter({hasText:'连接成功'}).waitFor();await page.locator('[data-model="save"]').click();await page.locator('#model-active').waitFor();assert.ok((await page.locator('.model-item').first().innerText()).includes('日常思考'))});
    await check('unsaved-changes-guard',async()=>{await page.locator('[data-model="edit"]').first().click();await page.locator('#model-name').fill('未保存名称');await page.keyboard.press('Escape');await page.locator('#model-keep').click();assert.equal(await page.locator('#model-name').inputValue(),'未保存名称');await page.locator('[data-model="back"]').click();await page.locator('#model-discard').click();await page.locator('#model-active').waitFor();assert.ok(!(await page.locator('.model-list').innerText()).includes('未保存名称'))});
    await check('switch-demo-does-not-change-history',async()=>{const before=await session(page);await page.locator('#model-active').selectOption('demo');await page.locator('#model-status').filter({hasText:'已切换'}).waitFor();await close();assert.ok((await page.locator('#model-chip').innerText()).includes('演示'));assert.deepEqual((await session(page)).cards,before.cards);await open()});
    await check('delete-connection-confirmation',async()=>{await page.locator('[data-model="edit"]').last().click();await page.locator('[data-model="delete"]').click();await page.locator('#model-delete-confirm').click();await page.locator('#model-active').waitFor();assert.equal(await page.locator('.model-item').count(),4)});
    await check('separate-workspace-has-no-keys',async()=>{const other=await browser.newContext();contexts.push(other);const p=await other.newPage();await p.goto(baseURL);await p.locator('#question').waitFor();await p.locator('#model-chip').click();await p.locator('.model-empty').waitFor();assert.equal(await p.locator('.model-item').count(),0)});
    await close();
    const mobileContext=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true,deviceScaleFactor:2});contexts.push(mobileContext);const mobile=await mobileContext.newPage();mobile.setDefaultTimeout(15000);mobile.on('pageerror',e=>errors.push(e.message));await mobile.goto(baseURL);await mobile.locator('#question').waitFor();
    await check('mobile-entry-editor-and-keyboard-layout',async()=>{await mobile.locator('#model-chip').click();await mobile.locator('[data-model="add"]').click();await mobile.locator('#model-name').fill('手机连接');await mobile.locator('#model-protocol').selectOption('anthropic');await mobile.locator('.model-advanced summary').click();assert.equal(await mobile.locator('#model-key').evaluate(el=>getComputedStyle(el).fontSize),'16px');assert.equal(await mobile.locator('#dialog').evaluate(el=>el.scrollWidth>el.clientWidth),false);await mobile.locator('#model-id').fill('test-model');await mobile.locator('[data-action="close-dialog"]').click();await mobile.locator('#model-discard').click();await mobile.locator('#dialog').waitFor({state:'hidden'})},mobile);
    await check('no-browser-errors',async()=>assert.deepEqual(errors,[]));
  } catch(error){if(!results.some(r=>r.status==='failed'))results.push({name:'models/setup',status:'failed',error:error.stack});}
  finally{for(const c of contexts)await c.close()}
  return results;
}
