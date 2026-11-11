'use strict';

// Shared browser/native conveniences. Native hosts inject a boolean marker only;
// remote content never receives filesystem, process or credential APIs.
(() => {
  const NOTE_KEY = 'gitchat.unsent-inspiration';
  let installPrompt = null;
  let updateWorker = null;
  let installing = false;
  let requestedUpdate = false;
  let workspaceStale = false;
  let exportFile = null;
  const EPOCH_KEY = 'gitchat.workspace-epoch';
  const query = selector => document.querySelector(selector);
  const html = value => String(value ?? '').replace(/[&<>"']/g, character => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[character]));
  const readNote = () => { try { return localStorage.getItem(NOTE_KEY) || ''; } catch { return ''; } };
  function saveNote(value) {
    try { if (value) localStorage.setItem(NOTE_KEY, value); else localStorage.removeItem(NOTE_KEY); return true; }
    catch { return false; }
  }
  function viewport() {
    const visible = window.visualViewport;
    if (visible && visible.scale !== 1) return; // Preserve the user's accessibility zoom.
    const height = visible?.height || innerHeight;
    document.documentElement.style.setProperty('--viewport-height', `${height}px`);
    document.documentElement.style.setProperty('--keyboard-height', `${Math.max(0, innerHeight - height)}px`);
    document.documentElement.classList.toggle('keyboard-open', innerHeight - height > 120);
  }
  window.addEventListener('resize', viewport);
  window.visualViewport?.addEventListener('resize', viewport);
  viewport();

  const offlineInput = query('#offline-note');
  if (offlineInput) {
    offlineInput.value = readNote();
    const save = () => {
      const ok = saveNote(offlineInput.value) || !offlineInput.value;
      query('#offline-save-status').textContent = ok ? '已留在当前设备，恢复连接后可继续编辑。' : '当前浏览器无法保存，请先复制这段文字。';
      return ok;
    };
    offlineInput.addEventListener('input', save);
    query('[data-platform="clear-note"]').addEventListener('click', () => { offlineInput.value = ''; save(); offlineInput.focus(); });
    query('#offline-note-form').addEventListener('submit', event => { event.preventDefault(); if (save()) location.href = '/'; else offlineInput.focus(); });
    return;
  }

  let epoch = '';
  try { epoch = localStorage.getItem(EPOCH_KEY) || ''; } catch {}
  const workspaceChannel = 'BroadcastChannel' in window ? new BroadcastChannel('gitchat-workspace') : null;
  function workspaceChanged(next) {
    if (!next || next === epoch) return;
    workspaceStale = true;
    const draft = query('#question')?.value || state.draft || '';
    const saved = !draft || saveNote(draft);
    modal('这个浏览器的空间已切换', `<p>另一标签页连接了新的空间。重新载入后再继续，避免把旧空间的内容写入新空间。</p>${draft ? `<p>${saved ? '未发送的问题已留在当前设备。' : '浏览器无法保存问题，请先复制下面的文字。'}</p><textarea readonly rows="4">${html(draft)}</textarea>` : ''}<div class="dialog-actions"><button class="send-button" data-platform="workspace-reload">重新载入</button></div>`);
  }
  window.gitchatPlatform = {
    assertWorkspace() {
      let latest = epoch;
      try { latest = localStorage.getItem(EPOCH_KEY) || ''; } catch {}
      if (latest && latest !== epoch) workspaceChanged(latest);
      if (workspaceStale) throw new Error('空间已切换，请重新载入后继续');
    },
    exportSession(snapshot) {
      const text = JSON.stringify(snapshot, null, 2);
      exportFile = new File([text], `gitchat-${snapshot.id}.json`, {type:'application/json'});
      let canShare = false;
      try { canShare = !!navigator.canShare?.({files:[exportFile]}); } catch {}
      modal('导出这次探索', `<p>这是包含卡片与分支关系的完整 JSON。${canShare ? '可通过系统分享保存到文件。' : '此设备可复制完整内容，保存为 .json 文件。'}</p><textarea id="export-json" readonly rows="8" aria-label="完整对话 JSON">${html(text)}</textarea><div class="dialog-actions"><button class="secondary" data-platform="copy-export">复制 JSON</button>${canShare ? '<button class="send-button" data-platform="share-export">保存或分享文件</button>' : ''}</div>`);
    },
  };
  window.addEventListener('storage', event => { if (event.key === EPOCH_KEY) workspaceChanged(event.newValue); });
  if (workspaceChannel) workspaceChannel.onmessage = event => workspaceChanged(event.data?.epoch);
  function publishWorkspaceChange() {
    epoch = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    try { localStorage.setItem(EPOCH_KEY, epoch); localStorage.removeItem('gitchat.session'); } catch {}
    workspaceChannel?.postMessage({epoch});
  }
  function keepDraft() {
    const draft = query('#question')?.value || state.draft || '';
    if (draft && !saveNote(draft)) throw new Error('无法保存未发送的问题，请先复制它再切换空间');
  }
  document.addEventListener('click', event => {
    if (workspaceStale && event.target.closest('[data-action],[data-platform]') && !event.target.closest('[data-platform="workspace-reload"]')) {
      event.preventDefault(); event.stopImmediatePropagation();
    }
  }, true);
  document.addEventListener('submit', event => { if (workspaceStale) { event.preventDefault(); event.stopImmediatePropagation(); } }, true);
  document.addEventListener('keydown', event => { if (workspaceStale && event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); } }, true);

  function nativeMode() {
    const native = window.__GITCHAT_NATIVE__ === true;
    document.documentElement.classList.toggle('native-app', native);
    query('[data-platform="install"]').hidden = native;
    query('[data-platform="native-settings"]').hidden = !native;
  }
  nativeMode();
  window.addEventListener('gitchat:native-ready', nativeMode);
  function updateStatus() {
    const status = query('#platform-status');
    if (!navigator.onLine) {
      status.innerHTML = '<span>网络暂时断开，已打开的卡片仍可阅读。问题可以先留在本机。</span><button data-platform="save-inspiration">保存当前灵感</button>';
      status.hidden = false;
    } else if (updateWorker) {
      status.innerHTML = '<span>求知有新版本，准备好时再更新。</span><button data-platform="update">更新</button>';
      status.hidden = false;
    } else status.hidden = true;
    query('[data-platform="recover-note"]').hidden = !readNote();
  }
  window.addEventListener('online', updateStatus);
  window.addEventListener('offline', updateStatus);
  window.addEventListener('storage', updateStatus);
  updateStatus();

  const installGuides = {
    android: ['Android', '用 Chrome 打开求知，点击浏览器菜单中的「安装应用」或「添加到主屏幕」。'],
    ios: ['iPhone / iPad', '在 Safari 中打开求知，点击分享按钮，选择「添加到主屏幕」，按提示添加。'],
    mac: ['macOS', 'Safari 可在「文件」菜单中选择「添加到程序坞」。Chrome 可使用地址栏的安装图标。'],
    windows: ['Windows', '在 Edge 或 Chrome 中打开求知，点击地址栏的安装图标；也可在浏览器菜单中选择安装此站点。'],
  };
  function detectPlatform() {
    const agent = navigator.userAgent;
    if (/Android/i.test(agent)) return 'android';
    if (/iPhone|iPad|iPod/i.test(agent) || /Macintosh/.test(agent) && navigator.maxTouchPoints > 1) return 'ios';
    return /Mac/i.test(agent) ? 'mac' : 'windows';
  }
  function showInstall(platform = detectPlatform()) {
    const [name, instructions] = installGuides[platform] || installGuides.windows;
    const standalone = matchMedia('(display-mode: standalone)').matches || navigator.standalone === true;
    modal('把求知留在手边', `<p>从主屏幕、程序坞或开始菜单打开，随时接着你的思路。</p><div class="platform-tabs" role="group" aria-label="选择设备">${Object.entries(installGuides).map(([key, guide]) => `<button data-platform="install-guide" data-os="${key}" aria-pressed="${key === platform}">${guide[0]}</button>`).join('')}</div><div class="platform-guide"><h3>${name}</h3><p>${instructions}</p></div>${!isSecureContext ? '<p class="platform-notice">安装需要 HTTPS 地址。局域网的普通 HTTP 地址可以浏览，但无法完整安装。</p>' : ''}${standalone ? '<p class="platform-notice">你已在独立窗口中使用求知。</p>' : installPrompt ? '<div class="dialog-actions"><button class="send-button" data-platform="install-now">安装到当前设备</button></div>' : '<p>如果没有安装选项，请使用上述浏览器菜单。安装后可在「设备与空间」连接已有空间。</p>'}`);
  }
  window.addEventListener('beforeinstallprompt', event => {
    event.preventDefault();
    installPrompt = event;
  });
  window.addEventListener('appinstalled', () => { installPrompt = null; toast('求知已安装'); });

  function showDevices() {
    if (state.busy) return toast('请先等待当前回答完成，或点击停止');
    const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname);
    modal('在另一台设备继续', `<p>让手机上的灵感，接着在电脑上展开。两台设备需打开同一个求知服务。</p><div class="device-origin"><span>当前服务</span><strong>${html(location.origin)}</strong></div>${loopback ? '<p class="platform-notice">这是当前电脑的本地空间。手机需要连接可访问的同一 HTTPS 服务，再进行配对。</p>' : ''}<div id="restore-space" hidden></div><div class="device-section"><h3>把这个空间连接到另一台设备</h3><p>生成一次性配对码，在另一台设备的「设备与空间」中输入。有效期 10 分钟，新码会使旧码失效。</p><button class="secondary" data-platform="create-pair">生成配对码</button><div id="pair-result" hidden></div></div><form id="device-pair-form" class="device-section"><h3>连接已有空间</h3><label for="device-code">另一台设备上的配对码</label><input id="device-code" required autocomplete="off" autocapitalize="off" spellcheck="false" maxlength="128" placeholder="粘贴一次性配对码"><label class="platform-checkbox"><input type="checkbox" id="pair-confirm" required><span>我知道本机将切换到该空间，原有对话不会自动合并，可以从这里返回原空间。</span></label><div class="dialog-actions"><button type="button" class="secondary" data-platform="refresh">刷新当前空间</button><button class="send-button" type="submit">连接空间</button></div></form>`);
    const restore = query('#restore-space');
    api('/api/devices').then(info => {
      if (!restore.isConnected || !info.can_restore) return;
      restore.hidden = false;
      restore.className = 'platform-notice';
      restore.innerHTML = '<p>本机已连接另一空间，原空间的访问凭据仍保留在此设备。返回后可再次连接；两个空间的对话不会自动合并。</p><button class="secondary" data-platform="restore-space">返回本机原空间</button>';
      query('#device-pair-form button[type="submit"]').disabled = true;
    }).catch(error => toast(error.message));
  }
  async function createPair(button) {
    button.disabled = true;
    const result = query('#pair-result');
    try {
      const pair = await post('/api/devices/pairing');
      if (!button.isConnected || !result?.isConnected) return;
      result.innerHTML = `<label for="device-pair-code">仅交给你自己的设备</label><input id="device-pair-code" readonly value="${html(pair.code)}" aria-label="一次性配对码"><p>有效至 ${html(new Date(pair.expires_at).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}))}。此码允许访问整个空间，使用后立即失效。</p><button class="secondary" data-platform="copy-pair">复制配对码</button>`;
      result.hidden = false;
      result.scrollIntoView({block:'nearest', behavior:matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth'});
    } finally { button.disabled = false; }
  }
  async function refreshSpace() {
    if (state.busy) return toast('请先等待当前回答完成，或点击停止');
    const sessionID = state.session?.id;
    const version = navigationVersion;
    await refreshSessions();
    if (sessionID && !state.shared) {
      const latest = await api(`/api/sessions/${encodeURIComponent(sessionID)}`);
      if (navigationVersion !== version || state.busy || state.session?.id !== sessionID) return;
      state.session = latest;
    }
    render();
    toast('已获取这个空间的最新卡片');
  }
  function recoverNote() {
    if (state.busy) return toast('请先等待当前回答完成，或点击停止');
    const note = readNote();
    if (!note) return;
    modal('接着这份灵感探索', `<p>这是你留在当前设备上的问题。可以复制，或放到新的提问卡片中修改后再发送。</p><textarea id="saved-inspiration" readonly rows="5">${html(note)}</textarea><div class="dialog-actions"><button class="secondary" data-platform="discard-note">丢弃</button><button class="send-button" data-platform="use-note">作为新的问题</button></div>`);
  }
  const platformActions = {
    install: () => showInstall(),
    'install-guide': button => showInstall(button.dataset.os),
    'install-now': async () => {
      if (!installPrompt || installing) return;
      installing = true;
      try {
        const prompt = installPrompt;
        installPrompt = null;
        await prompt.prompt();
        const choice = await prompt.userChoice;
        if (choice.outcome === 'accepted') closeDialog();
        else showInstall();
      } finally { installing = false; }
    },
    devices: showDevices,
    'workspace-reload': () => {
      try { localStorage.removeItem('gitchat.session'); } catch {}
      location.href = '/';
    },
    'restore-space': async button => {
      keepDraft();
      button.disabled = true;
      try {
        await post('/api/devices/restore');
        publishWorkspaceChange();
        location.href = '/';
      } catch (error) { button.disabled = false; throw error; }
    },
    'create-pair': createPair,
    'copy-pair': () => copyText(query('#device-pair-code').value),
    refresh: refreshSpace,
    'native-settings': () => {
      if (state.busy) return toast('请先等待当前回答完成，或点击停止');
      if (state.draft.trim()) return modal('先留住当前的问题', '<p>切换连接会离开当前页面。可以把未发送的问题留在这台设备，回来后继续编辑。</p><div class="dialog-actions"><button class="secondary" data-action="close-dialog">继续编辑</button><button class="send-button" data-platform="native-save-leave">保存灵感并切换</button></div>');
      if (window.__GITCHAT_NATIVE__ === true) location.href = 'gitchat://settings';
    },
    'native-save-leave': () => {
      if (!saveNote(state.draft)) return toast('无法保存，请先复制问题');
      if (window.__GITCHAT_NATIVE__ === true) location.href = 'gitchat://settings';
    },
    'native-reload': () => {
      if (state.busy) return toast('请先等待当前回答完成，或点击停止');
      if (state.draft.trim()) return modal('保留问题再刷新', '<p>未发送的问题会留在当前设备，刷新后可从导航中的灵感入口继续。</p><div class="dialog-actions"><button class="secondary" data-action="close-dialog">继续编辑</button><button class="send-button" data-platform="save-and-reload">保存并刷新</button></div>');
      location.reload();
    },
    'save-and-reload': () => { keepDraft(); location.reload(); },
    'copy-export': () => copyText(query('#export-json').value),
    'share-export': async () => {
      if (!exportFile) return;
      try { await navigator.share({files:[exportFile],title:'求知探索记录'}); }
      catch (error) { if (error.name !== 'AbortError') toast('系统分享未完成，可以复制 JSON 保存'); }
    },
    'save-inspiration': () => {
      const value = query('#question')?.value || state.draft;
      if (!value.trim()) return toast('先写下你想保留的问题');
      toast(saveNote(value) ? '灵感已保存在当前设备，联网后可从导航中继续' : '无法保存，请先复制这段文字');
      updateStatus();
    },
    'recover-note': recoverNote,
    'discard-note': () => { saveNote(''); closeDialog(); updateStatus(); },
    'use-note': () => {
      if (state.busy) return toast('请先等待当前回答完成，或点击停止');
      if (state.draft.trim()) return toast('当前还有未发送的问题，请先完成它，或复制这份灵感');
      const note = readNote();
      actions.new();
      state.draft = note;
      render();
      query('#question')?.focus();
      // Retain the note until the user explicitly discards it. Merely placing it
      // into a composer is not proof that it reached the server.
    },
    update: () => {
      if (state.busy || state.draft.trim()) return toast('先完成当前提问，再更新求知');
      requestedUpdate = true;
      if (updateWorker?.state === 'activated') location.reload();
      else updateWorker?.postMessage({type:'ACTIVATE_UPDATE'});
    },
  };
  document.addEventListener('click', event => {
    const button = event.target.closest('[data-platform]');
    if (!button || button.disabled || !platformActions[button.dataset.platform]) return;
    Promise.resolve(platformActions[button.dataset.platform](button)).catch(error => toast(error.message));
  });
  window.addEventListener('gitchat:native-request', event => {
    if (!window.__GITCHAT_NATIVE__ || workspaceStale) return;
    const action = event.detail?.action === 'settings' ? 'native-settings' : event.detail?.action === 'reload' ? 'native-reload' : null;
    if (action) Promise.resolve(platformActions[action]()).catch(error => toast(error.message));
  });
  document.addEventListener('submit', async event => {
    if (event.target.id !== 'device-pair-form') return;
    event.preventDefault();
    if (state.busy) return toast('请先等待当前回答完成，或点击停止');
    if (!query('#pair-confirm').checked) return;
    const button = event.target.querySelector('button[type="submit"]');
    button.disabled = true;
    try {
      keepDraft();
      await post('/api/devices/pair', {code:query('#device-code').value.trim()});
      publishWorkspaceChange();
      location.href = '/';
    } catch (error) { toast(error.message); button.disabled = false; }
  });

  if ('serviceWorker' in navigator && isSecureContext && !window.__GITCHAT_NATIVE__) {
    let controller = navigator.serviceWorker.controller;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (controller && requestedUpdate && !state.busy && !state.draft.trim()) location.reload();
      controller = navigator.serviceWorker.controller;
    });
    navigator.serviceWorker.register('/sw.js', {updateViaCache:'none'}).then(registration => {
      const waiting = () => {
        if (registration.waiting && navigator.serviceWorker.controller) {
          updateWorker = registration.waiting;
          updateStatus();
        }
      };
      waiting();
      registration.addEventListener('updatefound', () => {
        const worker = registration.installing;
        worker?.addEventListener('statechange', () => { if (worker.state === 'installed') waiting(); });
      });
    }).catch(() => { /* Browsing still works if private mode disallows installation. */ });
  }
})();
