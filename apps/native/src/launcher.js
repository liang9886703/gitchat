'use strict';

const form = document.querySelector('#connect-form');
const address = document.querySelector('#server-url');
const status = document.querySelector('#status');
let busy = false;

function message(text, error = false) {
  status.textContent = text;
  status.hidden = !text;
  status.classList.toggle('error', error);
}

async function connect(kind) {
  if (busy) return;
  if (kind === 'remote' && !form.reportValidity()) return;
  busy = true;
  document.querySelectorAll('button').forEach(button => { button.disabled = true; });
  address.disabled = true;
  message(kind === 'local' ? '正在打开本地空间…' : '正在安全连接你的空间…');
  try {
    const invoke = window.__TAURI__?.core?.invoke;
    if (!invoke) throw new Error('请在求知原生应用中打开连接页面。');
    await invoke(kind === 'local' ? 'connect_local' : 'connect_remote', kind === 'local' ? {} : { url: address.value.trim() });
    message('连接成功，正在进入思考空间…');
  } catch (error) {
    message(String(error?.message || error), true);
  } finally {
    busy = false;
    document.querySelectorAll('button').forEach(button => { button.disabled = false; });
    address.disabled = false;
  }
}

form.addEventListener('submit', event => { event.preventDefault(); connect('remote'); });
document.querySelector('#local-button').addEventListener('click', () => connect('local'));

async function init() {
  try {
    const invoke = window.__TAURI__?.core?.invoke;
    if (!invoke) { message('这是求知客户端的连接页面，请使用已安装的应用打开。', true); return; }
    const info = await invoke('connection_info');
    document.querySelector('#local-card').hidden = !info.desktop;
    if (!info.desktop) document.querySelector('#return-hint').textContent = '稍后可在应用菜单的「连接设置」中切换。';
    if (info.previous?.kind === 'remote') address.value = info.previous.url;
    if (info.error) { message(info.error, true); return; }
    if (new URLSearchParams(location.search).has('settings')) return;
    if (info.previous?.kind === 'remote' || info.previous?.kind === 'local' && info.desktop) await connect(info.previous.kind);
  } catch (error) { message(String(error?.message || error), true); }
}
init();
