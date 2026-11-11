'use strict';
(() => {
  const protocols = {
    'openai-chat': { label: 'OpenAI Chat Completions / 兼容服务', url: 'https://api.openai.com/v1', hint: '支持 OpenAI 兼容服务；模型 ID 请以你的服务商为准。' },
    'openai-responses': { label: 'OpenAI Responses', url: 'https://api.openai.com/v1', hint: '使用 Responses API；不在提供商端保存响应。' },
    anthropic: { label: 'Anthropic / Claude Messages', url: 'https://api.anthropic.com/v1', hint: '使用 Claude 原生 Messages 协议。' },
    gemini: { label: 'Google Gemini generateContent', url: 'https://generativelanguage.googleapis.com/v1beta', hint: '使用 Gemini 原生 generateContent 协议。' },
    ollama: { label: 'Ollama 本地模型', url: 'http://127.0.0.1:11434', hint: '本地地址指运行求知服务的电脑。请先在 Ollama 下载模型；本地部署支持内网地址。' },
  };
  let settings, generation = 0, controller, dirty = false, editing = false, writing = false, workspace;
  const dialog = document.querySelector('#dialog');
  const element = id => document.getElementById(id);
  const esc = escapeHTML;
  const active = () => dialog.open && dialog.classList.contains('model-dialog');
  function message(text, error = false) { const el = element('model-status'); if (!el) return; el.textContent = text; el.classList.toggle('error-box', error); el.hidden = !text; }
  function valid(ticket) { return active() && ticket === generation && workspace === state.config.workspace_id; }
  function panel(content) {
    controller?.abort(); generation++; dirty = false;
    modal('模型与服务', `${content}<p id="model-status" role="status" aria-live="polite" hidden></p>`);
    dialog.classList.add('model-dialog'); dialog.scrollTop = 0;
  }
  async function open() {
    workspace = state.config.workspace_id; editing = false;
    panel('<p>正在读取当前空间的模型连接…</p>'); const ticket = generation;
    try { const value = await api('/api/model-connections'); if (!valid(ticket)) return; settings = value; list(); }
    catch (error) { if (valid(ticket)) message(error.message, true); }
  }
  function list() {
    editing = false;
    const options = [{ id:'server', name:'服务端默认', detail:settings.server.model || '演示模式 · 本地示例' }, {id:'demo',name:'演示模式',detail:'仅体验卡片交互，不调用模型'}, ...settings.connections.map(c=>({...c,detail:`${protocols[c.protocol]?.label || c.protocol} · ${c.model}`}))];
    panel(`<p class="model-intro">让每张卡片连接你选择的模型。切换后用于下一张卡片，当前回答会继续完成。</p><label for="model-active">当前使用</label><select id="model-active">${options.map(c=>`<option value="${esc(c.id)}" ${settings.active_id===c.id?'selected':''}>${esc(c.name)}${c.model?' · '+esc(c.model):''}</option>`).join('')}</select><div class="model-list">${settings.connections.length ? settings.connections.map(c=>`<article class="model-item"><div><strong>${esc(c.name)} ${settings.active_id===c.id?'<span class="model-badge">使用中</span>':''}</strong><p>${esc(c.model)}<br>${esc(protocols[c.protocol]?.label)} · ${c.has_key?'密钥已保存':'无需密钥 / 未设置'}</p></div><button class="secondary" data-model="edit" data-id="${esc(c.id)}" aria-label="编辑 ${esc(c.name)}">编辑</button></article>`).join('') : '<div class="model-empty"><strong>接入你的第一个模型</strong><p>选择协议，填写地址与密钥，测试通过后开始探索。</p></div>'}</div><button class="send-button model-add" data-model="add">＋ 添加模型连接</button><p class="model-note">配置保存在当前空间所在的服务上；连接同一空间的设备共用。密钥只用于请求你填写的模型地址。</p>`);
  }
  function form(id) {
    const c = settings.connections.find(c=>c.id===id) || {id:'',name:'',protocol:'openai-chat',base_url:protocols['openai-chat'].url,model:'',max_tokens:4096,timeout_seconds:90,stream:true,json_mode:'native',token_parameter:'max_completion_tokens'};
    editing = true;
    panel(`<button class="model-back" data-model="back">← 返回连接列表</button><form id="model-form" data-id="${esc(c.id)}" autocomplete="off"><label for="model-name">连接名称</label><input id="model-name" required maxlength="60" value="${esc(c.name)}" placeholder="例如：日常思考 / 本地模型"><label for="model-protocol">模型协议</label><select id="model-protocol">${Object.entries(protocols).map(([id,p])=>`<option value="${id}" ${id===c.protocol?'selected':''}>${esc(p.label)}</option>`).join('')}</select><p id="model-hint" class="model-note">${esc(protocols[c.protocol]?.hint)}</p><label for="model-url">API 地址</label><input id="model-url" type="url" required maxlength="2048" value="${esc(c.base_url)}" spellcheck="false" autocapitalize="none"><label for="model-key">API Key ${c.has_key?'<span class="model-badge">已保存</span>':''}</label><input id="model-key" type="password" maxlength="8192" autocomplete="new-password" placeholder="${c.has_key?'留空保留已保存的密钥':'本地免鉴权服务可留空'}" spellcheck="false" autocapitalize="none">${c.has_key?'<label class="model-check"><input type="checkbox" id="model-clear-key"> 清除已保存的密钥</label>':''}<label for="model-id">模型 ID</label><div class="model-id-row"><input id="model-id" list="model-options" required maxlength="200" value="${esc(c.model)}" placeholder="读取列表或手动填写" spellcheck="false" autocapitalize="none"><button type="button" class="secondary" data-model="models">读取模型</button></div><datalist id="model-options"></datalist><p id="model-list-note" class="model-note">服务不支持列表时，可直接填写模型 ID。</p><details class="model-advanced"><summary>高级设置</summary><div class="model-columns"><div><label for="model-tokens">最大输出 tokens</label><input id="model-tokens" type="number" min="128" max="65536" required value="${c.max_tokens}"></div><div><label for="model-timeout">超时（秒）</label><input id="model-timeout" type="number" min="5" max="120" required value="${c.timeout_seconds}"></div></div><label class="model-check"><input id="model-stream" type="checkbox" ${c.stream?'checked':''}> 使用流式响应</label><label for="model-json">结构化回答</label><select id="model-json"><option value="native" ${c.json_mode==='native'?'selected':''}>JSON 模式（Claude 使用提示词约束）</option><option value="prompt" ${c.json_mode==='prompt'?'selected':''}>提示词兼容模式</option></select><p class="model-note">服务不支持 JSON 参数时可使用提示词兼容模式；卡片内容仍会校验。</p><div id="model-token-wrap" ${c.protocol==='openai-chat'?'':'hidden'}><label for="model-token-param">Chat Completions 输出参数</label><select id="model-token-param"><option value="max_completion_tokens" ${c.token_parameter==='max_completion_tokens'?'selected':''}>max_completion_tokens</option><option value="max_tokens" ${c.token_parameter!=='max_completion_tokens'?'selected':''}>max_tokens（兼容服务常用）</option></select></div></details><p class="model-note">测试会发送一条简短请求，可能产生服务商费用；不发送你的对话记录。</p><div class="model-actions"><button type="button" class="secondary" data-model="test">测试连接</button><button type="button" class="secondary" data-model="save">仅保存</button><button type="submit" class="send-button">保存并使用</button></div>${c.id?`<button type="button" class="model-delete danger" data-model="delete" ${settings.active_id===c.id?'disabled':''}>删除连接</button>${settings.active_id===c.id?'<p class="model-note">请先切换其他模型，再删除当前连接。</p>':''}`:''}</form><div id="model-confirm" hidden></div>`);
    element('model-name').focus();
  }
  function input(requireModel = true) {
    const form = element('model-form');
    element('model-id').required = requireModel;
    if (!form.reportValidity()) return null;
    return {id:form.dataset.id,name:element('model-name').value.trim(),protocol:element('model-protocol').value,base_url:element('model-url').value.trim(),model:element('model-id').value.trim(),api_key:element('model-key').value,clear_key:!!element('model-clear-key')?.checked,max_tokens:Number(element('model-tokens').value),timeout_seconds:Number(element('model-timeout').value),stream:element('model-stream').checked,json_mode:element('model-json').value,token_parameter:element('model-token-param').value,revision:settings.revision};
  }
  function lock(value) { element('model-form')?.querySelectorAll('input,select,button').forEach(el=>{ if(value) el.dataset.wasDisabled=String(el.disabled); el.disabled=value || el.dataset.wasDisabled==='true'; }); writing=value; }
  async function refresh() { const config = await api('/api/config'); if (config.workspace_id!==workspace) throw new Error('空间已切换，请刷新页面'); state.config=config;renderSidebar(); const chip=element('model-chip'); if(chip) chip.textContent=config.mode==='demo'?'演示 · 配置模型':config.model || '模型设置'; render(); }
  async function save(activate) {
    const body = input(); if(!body || writing) return; controller=null; const ticket=generation;lock(true);message('正在保存…');
    try { const result=await api('/api/model-connections'+(body.id?'/'+encodeURIComponent(body.id):''),{method:body.id?'PUT':'POST',body:JSON.stringify({...body,activate})});if(!valid(ticket))return;settings=result;dirty=false;element('model-key').value='';await refresh();if(valid(ticket)){list();message(activate?'已启用，新卡片将使用这个模型。':'连接已保存。');} }
    catch(error){if(valid(ticket))message(error.message,true)}finally{lock(false)}
  }
  async function probe(operation) {
    const body=input(operation!=='models');if(!body || writing)return;controller=new AbortController();const ticket=generation;lock(true);message(operation==='models'?'正在读取模型列表…':'正在验证模型返回的卡片内容…');
    try {const result=await api('/api/model-connections/'+operation,{method:'POST',body:JSON.stringify(body),signal:controller.signal});if(!valid(ticket))return;
      if(operation==='models'){element('model-options').innerHTML=result.models.map(model=>`<option value="${esc(model)}"></option>`).join('');element('model-list-note').textContent=`已读取 ${result.models.length} 个模型${result.more?'（还有更多，可手动填写）':''}。在模型 ID 输入框选择。`;if(!element('model-id').value){element('model-id').value=result.models[0];dirty=true;}message('模型列表已更新。');}
      else message(`连接成功 · ${result.model} · ${(result.latency_ms/1000).toFixed(1)} 秒\n${result.answer}`);
    }catch(error){if(valid(ticket)&&error.name!=='AbortError')message(error.message,true)}finally{controller=null;lock(false)}
  }
  function confirmDiscard(next) {
    if(writing){controller?.abort();if(!controller){message('正在保存，请稍候');return;}}
    if(!dirty){next();return;}
    const box=element('model-confirm');box.hidden=false;box.innerHTML='<p>还有未保存的配置。</p><button class="secondary" id="model-keep">继续编辑</button> <button class="secondary danger" id="model-discard">放弃修改</button>';box.scrollIntoView({block:'nearest'});element('model-keep').focus();element('model-keep').onclick=()=>{box.hidden=true};element('model-discard').onclick=()=>{dirty=false;next()};
  }
  document.addEventListener('click',event=>{
    const target=event.target.closest('[data-model]');if(!target||target.disabled)return;event.preventDefault();
    const action=target.dataset.model;
    const work=async()=>{
      if(action==='open')return open();if(action==='add')return form();if(action==='edit')return form(target.dataset.id);if(action==='back')return confirmDiscard(list);
      if(action==='save')return save(false);if(action==='test'||action==='models')return probe(action);
      if(action==='delete'){const id=element('model-form').dataset.id;const box=element('model-confirm');box.hidden=false;box.innerHTML='<p>删除此连接？已有卡片会保留。</p><button class="secondary danger" id="model-delete-confirm">确认删除</button>';element('model-delete-confirm').onclick=async()=>{if(writing)return;const ticket=generation;lock(true);try{const result=await api('/api/model-connections/'+encodeURIComponent(id),{method:'DELETE',body:JSON.stringify({revision:settings.revision})});if(valid(ticket)){settings=result;list();message('连接已删除。')}}catch(error){if(valid(ticket))message(error.message,true)}finally{lock(false)}};}
    };work().catch(error=>message(error.message,true));
  });
  document.addEventListener('submit',event=>{if(event.target.id==='model-form'){event.preventDefault();save(true)}});
  dialog.addEventListener('input',event=>{if(event.target.closest('#model-form'))dirty=true});
  dialog.addEventListener('change',async event=>{
    if(event.target.id==='model-protocol') {const p=protocols[event.target.value];element('model-url').value=p.url;element('model-hint').textContent=p.hint;element('model-token-wrap').hidden=event.target.value!=='openai-chat';element('model-id').value='';element('model-options').innerHTML='';element('model-list-note').textContent='服务不支持列表时，可直接填写模型 ID。';dirty=true;}
    if(event.target.id==='model-active') {const select=event.target;select.disabled=true;const ticket=generation;try{const result=await api('/api/model-selection',{method:'PUT',body:JSON.stringify({id:select.value,revision:settings.revision})});if(!valid(ticket))return;settings=result;await refresh();if(valid(ticket)){list();message('已切换，下一张卡片将使用所选模型。')}}catch(error){if(valid(ticket)){select.value=settings.active_id;message(error.message,true)}}finally{select.disabled=false}}
  });
  document.addEventListener('click',event=>{if(!active())return;if(event.target===dialog||event.target.closest('[data-action="close-dialog"]')){event.stopImmediatePropagation();event.preventDefault();confirmDiscard(()=>dialog.close())}},true);
  dialog.addEventListener('cancel',event=>{if(active()){event.preventDefault();confirmDiscard(()=>dialog.close())}});
  dialog.addEventListener('close',()=>{if(!dialog.classList.contains('model-dialog'))return;controller?.abort();controller=null;generation++;element('model-key')&&(element('model-key').value='');dialog.classList.remove('model-dialog');editing=false;dirty=false;writing=false;});
})();
