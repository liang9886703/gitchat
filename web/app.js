'use strict';

const $ = (selector, root = document) => root.querySelector(selector);
const state = {
  config: { mode: 'demo', apps: [] }, sessions: [], session: null, focus: null,
  view: 'focus', collection: 'sessions', selected: new Set(), compose: false,
  operation: 'deepen', quote: '', draft: '', busy: false, streamCard: null,
  shared: null, collectionCards: [], selection: '', pending: null,
  graphAll: false, graphPages: {},
};
let navigationVersion = 0;
const pendingDraftKey = 'gitchat.pending-question';
function savePendingDraft(question, sessionID = state.session?.id) {
  try { sessionStorage.setItem(pendingDraftKey, JSON.stringify({question, sessionID, focus:state.focus, operation:state.operation, quote:state.quote, selected:[...state.selected], existing:realCards().map(card=>card.id), workspace:state.config.workspace_id})); } catch { /* In-memory input remains available when browser storage is unavailable. */ }
}
function clearPendingDraft() { try { sessionStorage.removeItem(pendingDraftKey); } catch {} }
async function restorePendingDraft() {
  let saved;
  try { saved = JSON.parse(sessionStorage.getItem(pendingDraftKey) || 'null'); } catch { return; }
  if (!saved || saved.workspace !== state.config.workspace_id || !saved.question || state.shared) return;
  if (saved.sessionID && state.sessions.some(item=>item.id===saved.sessionID)) {
    if (state.session?.id !== saved.sessionID) acceptSession(await api(`/api/sessions/${encodeURIComponent(saved.sessionID)}`));
    const recovered = realCards().find(card=>!saved.existing?.includes(card.id) && card.question===saved.question);
    if (recovered) { state.focus=recovered.id; clearPendingDraft(); return; }
    state.focus = cardByID(saved.focus) ? saved.focus : state.session.root_id;
  } else { state.session=null; state.focus=null; }
  state.draft = saved.question;
  state.quote = saved.quote || '';
  state.operation = saved.operation || 'deepen';
  state.selected = new Set((saved.selected || []).filter(cardByID));
  state.compose = !!state.session;
  state.view = 'focus';
}

const palette = [
  ['#eee8fc', '#66508e'], ['#e7e9ff', '#51569a'], ['#e3f1e9', '#386b53'],
  ['#fff0df', '#805c34'], ['#f5e3ec', '#8b4c6e'], ['#e0f0f3', '#386a76'],
];
const statuses = { pending: '正在准备', streaming: '正在思考', completed: '已完成', cancelled: '已停止', error: '生成失败' };
const operations = { deepen: '深入思考', branch: '换个方向', merge: '合并思考' };
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const id = value => escapeHTML(value);
function requestID() {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = [...bytes].map(byte => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
const short = (value, length = 26) => Array.from(String(value || '')).slice(0, length).join('');
const cardByID = cardID => state.session?.cards?.find(card => card.id === cardID);
const current = () => cardByID(state.focus);
const isRoot = card => !card || card.id === state.session?.root_id;
const realCards = () => (state.session?.cards || []).filter(card => card.id !== state.session.root_id);
const color = depth => {
  const colors = palette[Math.max(0, Number(depth || 0) - 1) % palette.length];
  return `--depth:${colors[0]};--depth-ink:${colors[1]}`;
};

async function api(path, options = {}) {
  window.gitchatPlatform?.assertWorkspace?.();
  const response = await fetch(path, {
    credentials: 'same-origin', ...options,
    headers: { 'Content-Type': 'application/json', ...(state.config.workspace_id ? { 'X-GitChat-Workspace': state.config.workspace_id } : {}), ...(options.headers || {}) },
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `请求失败（${response.status}）`);
  return body;
}
const post = (path, body = {}) => api(path, { method: 'POST', body: JSON.stringify(body) });
function toast(message) {
  const element = $('#toast');
  element.textContent = message;
  element.classList.add('visible');
  const dialogStatus = $('#dialog-status');
  if ($('#dialog').open && dialogStatus) {
    dialogStatus.textContent = message;
    dialogStatus.hidden = false;
  }
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => element.classList.remove('visible'), 4300);
}
function closeSidebar() {
  $('#sidebar').classList.remove('open');
  $('#sidebar-scrim').classList.remove('open');
}
function modal(title, content) {
  closeSidebar();
  $('#dialog-content').innerHTML = `<div class="dialog-head"><h2 id="dialog-title">${escapeHTML(title)}</h2><button data-action="close-dialog" aria-label="关闭">×</button></div>${content}<p id="dialog-status" class="dialog-status" role="status" hidden></p>`;
  if (!$('#dialog').open) $('#dialog').showModal();
}
function closeDialog() { $('#dialog').close(); }

function inlineMarkdown(text) {
  return escapeHTML(text)
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1 ↗</a>');
}
function markdown(text) {
  const segments = String(text || '').split(/```[^\n]*\n([\s\S]*?)```/g);
  return segments.map((segment, index) => {
    if (index % 2) return `<pre><code>${escapeHTML(segment)}</code></pre>`;
    return segment.split(/\n\s*\n/).map(block => {
      if (/^#{1,4} /.test(block)) return `<h3>${inlineMarkdown(block.replace(/^#{1,4} /, ''))}</h3>`;
      if (/^[-*] /m.test(block) && block.split('\n').every(line => /^[-*] /.test(line))) {
        return `<ul>${block.split('\n').map(line => `<li>${inlineMarkdown(line.slice(2))}</li>`).join('')}</ul>`;
      }
      return `<p>${inlineMarkdown(block).replace(/\n/g, '<br>')}</p>`;
    }).join('');
  }).join('');
}

function ancestry(card) {
  const visited = new Set();
  const path = [];
  function visit(node) {
    if (!node || visited.has(node.id)) return;
    visited.add(node.id);
    (node.parent_ids || []).forEach(parentID => visit(cardByID(parentID)));
    if (!isRoot(node)) path.push(node);
  }
  visit(card);
  return path.sort((a, b) => a.depth - b.depth);
}
function siblings(card) {
  if (!card) return [];
  return realCards().filter(candidate => !candidate.archived && candidate.depth === card.depth &&
    candidate.parent_ids?.some(parentID => card.parent_ids?.includes(parentID)));
}
function composeParents(card = current()) {
  if (state.operation === 'merge') return [...state.selected].map(cardByID).filter(Boolean);
  if (state.operation === 'branch') return (card?.parent_ids || [state.session?.root_id]).map(cardByID).filter(Boolean);
  return card ? [card] : [];
}
function composeDepth(card = current()) {
  return Math.max(0, ...composeParents(card).map(parent => parent.depth || 0)) + 1;
}
function suggestionHTML(card, limit = 3) {
  const suggestions = (card?.suggestions || []).slice(0, limit);
  if (!suggestions.length) return '';
  return `<div class="suggestions"><div class="section-label">沿着好奇，继续探索</div><div class="suggestion-list">${suggestions.map((suggestion, index) => `<button class="suggestion" data-action="suggestion" data-index="${index}" ${state.busy ? 'disabled' : ''}><span>＋</span><span>${escapeHTML(suggestion.title || suggestion.question)}</span><span>↗</span></button>`).join('')}</div></div>`;
}
function composerHTML() {
  return `<form class="composer" id="chat-form"><textarea id="question" name="question" aria-label="你的问题" enterkeyhint="enter" placeholder="${state.operation === 'merge' ? '想如何比较或汇总这些思路？' : state.operation === 'branch' ? '还有什么值得探索的方向？' : state.session ? '沿着这个思路，继续问…' : '你正在好奇什么？'}" rows="2" maxlength="12000" ${state.busy ? 'disabled' : ''}>${escapeHTML(state.draft)}</textarea><div class="composer-bottom"><div class="operation-picker">${state.operation === 'merge' ? '<button type="button" class="active">⌘ 合并思考</button>' : `<button type="button" data-action="operation" data-value="deepen" class="${state.operation === 'deepen' ? 'active' : ''}">↑ 深入</button><button type="button" data-action="operation" data-value="branch" class="${state.operation === 'branch' ? 'active' : ''}" ${!state.session ? 'disabled' : ''}>↔ 发散</button>`}</div>${state.busy ? '<button class="send-button stop" type="button" data-action="stop">■ 停止</button>' : `<button class="send-button" type="submit" ${state.operation === 'merge' && state.selected.size < 2 ? 'disabled' : ''}>开始思考 ↑</button>`}</div></form>`;
}
function renderSidebar() {
  $('#sessions').innerHTML = state.sessions.map(session => `<button class="session-item ${session.id === state.session?.id && !state.shared ? 'active' : ''}" data-action="session" data-id="${id(session.id)}" title="${escapeHTML(session.title)}">${escapeHTML(session.title || '新的探索')}</button>`).join('') || '<div class="session-item">还没有对话</div>';
  document.querySelectorAll('.collections button').forEach(button => button.classList.toggle('active', button.dataset.value === state.collection));
  $('#mode').textContent = state.config.mode === 'demo' ? '演示模式 · 本地示例回答' : `${state.config.connection_name || '模型已配置'} · ${state.config.model || '私人工作区'}`;
  if ($('#model-chip')) $('#model-chip').textContent = state.config.mode === 'demo' ? '演示 · 配置模型' : state.config.model || '模型设置';
  $('#footer-mode').textContent = state.config.mode === 'demo' ? '演示回答用于体验交互' : 'AI 生成内容，请核实重要信息';
}
function render() {
  const previousAnswer = $('#answer-card');
  const previousGraph = $('#graph-scroll');
  const graphPosition = previousGraph ? { left: previousGraph.scrollLeft, top: previousGraph.scrollTop } : null;
  const lanePositions = new Map([...document.querySelectorAll('.graph-lane')].map(lane => [lane.dataset.depth, lane.scrollLeft]));
  renderSidebar();
  $('#page-title').textContent = state.collection === 'favorites' ? '收藏的卡片' : state.collection === 'archive' ? '归档与历史' : state.session?.title || '新的探索';
  $('#page-subtitle').textContent = state.shared ? '来自分享的思考路径' : state.session ? `${realCards().length} 张卡片 · ${state.view === 'overview' ? '让思路彼此相连' : '当前分支的上下文已就绪'}` : '一个问题，一个新的方向';
  document.querySelectorAll('.view-switch button').forEach(button => {
    button.classList.toggle('active', button.dataset.value === state.view);
    button.setAttribute('aria-pressed', button.dataset.value === state.view);
  });
  if (state.collection !== 'sessions') return renderCollection();
  let html = state.shared ? '<div class="shared-banner"><span>这是一份分享的对话快照</span><button class="send-button" data-action="import-current">复制并继续探索 ↗</button></div>' : '';
  if (state.pending) html += pendingHTML();
  else if (!state.session || realCards().length === 0) html += welcomeHTML();
  else if (state.view === 'overview') html += overviewHTML();
  else html += focusHTML();
  $('#main').innerHTML = html;
  const nextAnswer = $('#answer-card');
  if (previousAnswer && nextAnswer && (previousAnswer.dataset.cardId === nextAnswer.dataset.cardId || previousAnswer.dataset.cardId === 'pending' && state.busy)) {
    syncElement(previousAnswer, nextAnswer);
    nextAnswer.replaceWith(previousAnswer);
  }
  if (state.view === 'overview') requestAnimationFrame(() => {
    const area = $('#graph-scroll');
    if (!area) return;
    area.querySelectorAll('.graph-lane').forEach(lane => {
      const anchor = lane.querySelector('.graph-node.current') || lane.querySelector('.graph-node.path-anchor') || lane.querySelector('.graph-node');
      if (lanePositions.has(lane.dataset.depth)) lane.scrollLeft = lanePositions.get(lane.dataset.depth);
      else if (anchor) centerInLane(lane, anchor);
      lane.addEventListener('scroll', drawGraph, { passive: true });
    });
    if (graphPosition) {
      area.scrollLeft = graphPosition.left;
      area.scrollTop = graphPosition.top;
    } else {
      const node = [...area.querySelectorAll('[data-node]')].find(element => element.dataset.node === state.focus);
      if (node) {
        const bounds = node.getBoundingClientRect();
        const viewport = area.getBoundingClientRect();
        area.scrollLeft += bounds.left + bounds.width / 2 - viewport.left - area.clientWidth / 2;
      }
    }
    drawGraph();
  });
}
// Keep the active card and its text nodes alive while its answer grows. This
// preserves selection and reading position instead of replacing the whole view.
function syncElement(target, source) {
  if (target.nodeType === Node.TEXT_NODE && source.nodeType === Node.TEXT_NODE) {
    if (source.data.startsWith(target.data)) target.appendData(source.data.slice(target.data.length));
    else if (target.data !== source.data) target.data = source.data;
    return;
  }
  if (target.nodeType !== Node.ELEMENT_NODE) return;
  for (const attribute of [...target.attributes]) if (!source.hasAttribute(attribute.name)) target.removeAttribute(attribute.name);
  for (const attribute of [...source.attributes]) if (target.getAttribute(attribute.name) !== attribute.value) target.setAttribute(attribute.name, attribute.value);
  const children = [...source.childNodes];
  children.forEach((child, index) => {
    const existing = target.childNodes[index];
    if (!existing) target.append(child.cloneNode(true));
    else if (existing.nodeType !== child.nodeType || existing.nodeName !== child.nodeName) existing.replaceWith(child.cloneNode(true));
    else syncElement(existing, child);
  });
  while (target.childNodes.length > children.length) target.lastChild.remove();
}
function updateAnswer(card) {
  const element = $('#answer-card');
  if (!element || element.dataset.cardId !== card.id) return render();
  const template = document.createElement('template');
  template.innerHTML = answerHTML(card);
  syncElement(element, template.content.firstElementChild);
}
function pendingHTML() {
  return `<div class="focus-layout"><section class="card-column"><div class="breadcrumb"><span>${operations[state.pending.operation]}</span><span>／</span><span>第 ${state.pending.depth} 层</span></div><div class="card-stack">${answerHTML(state.pending)}</div><div class="nav-actions"><button class="send-button stop" data-action="stop">■ 停止生成</button></div></section><aside class="path-rail"><div class="rail-memory"><strong>把这个问题想清楚</strong>正在整理当前思路，回答会出现在这张卡片里。</div></aside></div>`;
}
function centerInLane(lane, node) {
  const bounds = node.getBoundingClientRect();
  const viewport = lane.getBoundingClientRect();
  lane.scrollLeft += bounds.left + bounds.width / 2 - viewport.left - lane.clientWidth / 2;
}
function welcomeHTML() {
  return `<section class="welcome"><div class="welcome-mark">✳</div><h1>从一个问题开始</h1><p>把大的问题拆小，把不同的思路展开。<br>每一张卡片，都是你探索的一步。</p>${state.config.mode === 'demo' ? '<div class="model-welcome"><span>当前是演示回答。接入模型，让思考真正开始。</span><button class="secondary" data-model="open">连接模型 ↗</button></div>' : ''}${composerHTML()}<div class="starter-grid"><button class="starter" data-action="starter" data-value="我想安排一次日本旅行，如何先确定方向？"><span>↗</span><strong>规划一段旅程</strong><small>从想去的地方<br>到自己的旅行路线</small></button><button class="starter" data-action="starter" data-value="我想了解大模型的长期记忆，应该从哪里开始？"><span>✧</span><strong>理解一个新概念</strong><small>先建立全貌<br>再逐层深入</small></button><button class="starter" data-action="starter" data-value="我需要为一个新项目选择技术方案，该怎样比较不同架构？"><span>⌘</span><strong>比较不同的方案</strong><small>分别探索可能性<br>再汇合成一个决定</small></button></div><div class="intro-hint">上滑深入 · 左滑发散 · 双指缩放查看思维图</div></section>`;
}
function focusHTML() {
  let card = current();
  if (state.compose && isRoot(card)) return welcomeHTML();
  if (isRoot(card) || card.archived) card = realCards().filter(item => !item.archived).at(-1);
  if (!card) { state.focus = state.session.root_id; return welcomeHTML(); }
  state.focus = card.id;
  const path = ancestry(card);
  const previous = cardByID(card.parent_ids?.[0]);
  const breadcrumb = `<div class="breadcrumb"><button data-action="view" data-value="overview">所有思路</button><span>／</span>${previous && !isRoot(previous) ? `<button data-action="focus" data-id="${id(previous.id)}">${escapeHTML(short(previous.title || previous.question, 18))}</button><span>／</span>` : ''}<span>${state.compose ? operations[state.operation] : `第 ${card.depth} 层`}</span></div>`;
  const cardContent = state.compose && !state.busy ? composeHTML(card) : answerHTML(card);
  const navigation = `<div class="nav-actions">${!state.shared ? `<button data-action="compose" data-value="branch" ${state.busy ? 'disabled' : ''}>← 换个方向</button>` : ''}<button data-action="parent" ${!previous || isRoot(previous) ? 'disabled' : ''}>↓ 上一步</button><button data-action="sibling" ${siblings(card).length < 2 ? 'disabled' : ''}>→ 相邻思路</button><button data-action="quote" ${state.shared || state.busy ? 'disabled' : ''}>引用选中文字</button></div>`;
  const goal = card.memory?.goal;
  return `<div class="focus-layout"><section class="card-column">${breadcrumb}<div class="card-stack ${previous && !isRoot(previous) ? 'has-parent' : ''} ${siblings(card).length > 1 ? 'has-sibling' : ''}">${cardContent}</div>${!state.compose && !state.shared ? suggestionHTML(card) : ''}${!state.compose && !state.shared ? state.busy ? '<div class="nav-actions"><button class="send-button stop" data-action="stop">■ 停止生成</button></div>' : '<button class="suggestion continue-prompt" data-action="compose" data-value="deepen"><span>↑</span><span>继续追问，深入这个想法</span><span>↗</span></button>' : ''}${navigation}</section><aside class="path-rail" aria-label="当前思路"><div class="rail-heading"><span>这条思考路径</span><button data-action="view" data-value="overview" aria-label="打开思维图">↗</button></div>${path.map(item => `<div class="path-item ${item.id === card.id ? 'current' : ''}"><button data-action="focus" data-id="${id(item.id)}">${escapeHTML(short(item.title || item.question, 34))}</button><small>第 ${item.depth} 层${item.success ? ' · 已找到答案' : ''}</small></div>`).join('')}${goal ? `<div class="rail-memory"><strong>记住你的目标</strong>${escapeHTML(goal)}</div>` : ''}</aside></div>`;
}
function composeHTML(card) {
  const parents = composeParents(card);
  const depth = composeDepth(card);
  const visibleParents = parents.filter(parent => !isRoot(parent));
  const context = visibleParents.length ? visibleParents.map(parent => `<span class="context-chip" data-source="${id(parent.id)}">${escapeHTML(short(parent.title || parent.question, 28))}${state.operation === 'merge' ? `<button type="button" data-action="remove-source" data-id="${id(parent.id)}" aria-label="移除 ${escapeHTML(parent.title || parent.question)}">×</button>` : ''}</span>`).join('') : '<span class="context-empty">新的独立方向，从自己的问题开始</span>';
  return `<article class="thought-card compose-card" style="${color(depth)}" data-depth="${depth}"><div class="card-topline"><span class="depth-label">${operations[state.operation]} · 第 ${depth} 层</span><span class="card-kind">提问</span><button class="icon-button" data-action="cancel-compose" aria-label="返回回答">×</button></div><h1>${state.operation === 'merge' ? '把不同的思路，放在一起' : state.operation === 'branch' ? '试着换一个方向' : '接下来，你想了解什么？'}</h1><div class="context-heading">${state.operation === 'merge' ? `带着 ${parents.length} 条思路，寻找共同的答案` : state.operation === 'branch' ? '沿用共同的背景，独立探索' : '从这里继续'}</div><div class="compose-context">${context}</div>${state.quote ? `<div class="quote">“${escapeHTML(state.quote)}” <button data-action="clear-quote" aria-label="移除引用">×</button></div>` : ''}${state.operation !== 'merge' ? suggestionHTML(card, 2) : ''}${state.operation === 'merge' && parents.length < 2 ? '<p class="compose-guidance" role="status">至少保留两张卡片才能合并；可以返回思维图继续选择。</p>' : ''}${composerHTML()}</article>`;
}
function answerHTML(card) {
  const busy = ['pending', 'streaming'].includes(card.status);
  return `<article class="thought-card" style="${color(card.depth)}" id="answer-card" data-card-id="${id(card.id)}" aria-busy="${busy}"><div class="card-topline"><span class="depth-label">第 ${card.depth} 层</span><span class="card-kind">${card.operation === 'merge' ? '汇合的思路' : '探索卡片'}${card.success ? ' · 已找到答案' : ''}</span><button class="icon-button ${card.favorite ? 'selected' : ''}" data-action="favorite" aria-label="${card.favorite ? '取消收藏' : '收藏卡片'}" ${state.shared || card.id === 'pending' ? 'disabled' : ''}>${card.favorite ? '★' : '☆'}</button><button class="icon-button" data-action="card-menu" aria-label="卡片操作" ${card.id === 'pending' ? 'disabled' : ''}>···</button></div><h1>${escapeHTML(card.title || card.question || '正在思考…')}</h1><div class="question-caption" ${!card.title || card.title === card.question ? 'hidden' : ''}>${escapeHTML(card.question)}</div><div class="answer" id="answer-content">${markdown(card.answer) || '<p><span class="streaming-dot"></span>正在整理这条思路…</p>'}</div><div class="error-box" ${card.error ? '' : 'hidden'}>${escapeHTML(card.error)}${card.error && !state.shared ? '<br><button data-action="retry">重新尝试 ↗</button>' : ''}</div><div class="card-bottom"><span>${busy ? '<span class="streaming-dot"></span>' : ''}${escapeHTML(statuses[card.status] || card.status)}${card.model ? ` · ${escapeHTML(card.model)}` : ''}</span><button data-action="success" ${state.shared || busy ? 'disabled' : ''}>${card.success ? '✓ 已找到答案' : '✓ 这个问题想清楚了'}</button></div></article>`;
}
function graphNodeHTML(card, pathIDs) {
  if (isRoot(card)) return `<button class="graph-node root" data-node="${id(card.id)}" data-action="compose-root" ${state.shared ? 'disabled' : ''}>探索的起点</button>`;
  const onPath = pathIDs.has(card.id);
  return `<article class="graph-node ${card.id === state.focus ? 'current' : ''} ${onPath ? 'path-anchor' : ''} ${state.selected.has(card.id) ? 'selected' : ''}" data-node="${id(card.id)}" style="${color(card.depth)}" data-action="graph-focus" data-id="${id(card.id)}" tabindex="0" role="button" aria-label="打开 ${escapeHTML(card.title || card.question)}">${!state.shared ? `<label class="node-select"><input type="checkbox" data-action="select" data-id="${id(card.id)}" aria-label="选择 ${escapeHTML(card.title || card.question)} 用于合并" ${state.selected.has(card.id) ? 'checked' : ''} ${state.busy ? 'disabled' : ''}><span aria-hidden="true">选择</span></label>` : ''}<span class="depth-label">${card.depth} 层${card.favorite ? ' · ★' : ''}${card.success ? ' · ✓' : ''}</span><h2>${escapeHTML(card.title || card.question)}</h2><p>${escapeHTML(card.answer || statuses[card.status])}</p>${card.parent_ids?.length > 1 ? `<small class="node-relation">汇合 ${card.parent_ids.length} 条思路</small>` : onPath ? '<small class="node-relation">当前思考路径</small>' : ''}</article>`;
}
function overviewHTML() {
  const focusPath = ancestry(current());
  const pathIDs = new Set(focusPath.map(card => card.id));
  const cards = (state.session.cards || []).filter(card => !card.archived);
  const depths = [...new Set(cards.map(card => card.depth))].sort((a, b) => a - b);
  const predictions = current()?.suggestions || [];
  const ghosts = !state.shared && predictions.length ? `<section class="graph-level prediction-level"><div class="graph-level-heading"><span>从当前卡片继续 · 尚未探索</span></div><div class="graph-lane" data-depth="predictions"><div class="graph-row">${predictions.map((suggestion, index) => `<button class="graph-node ghost" data-parent="${id(state.focus)}" data-action="suggestion" data-index="${index}" ${state.busy ? 'disabled' : ''}><span>＋ 待探索</span><h2>${escapeHTML(suggestion.title)}</h2></button>`).join('')}</div></div></section>` : '';
  const rows = depths.map(depth => {
    const nodes = cards.filter(card => card.depth === depth);
    const pathNodes = nodes.filter(card => pathIDs.has(card.id));
    const preferred = nodes.findIndex(card => card.id === state.focus || card.id === pathNodes.at(-1)?.id);
    const anchor = Math.max(0, Math.min(nodes.length - 1, state.graphPages[depth] ?? Math.max(0, preferred)));
    const start = Math.max(0, Math.min(nodes.length - 3, anchor - 1));
    let visible = state.graphAll ? nodes : nodes.slice(start, start + 3);
    // All ancestors of a merge carry meaning. Keep them together until the user
    // explicitly pages away, even when a merge has more than three sources.
    if (!state.graphAll && state.graphPages[depth] === undefined && pathNodes.length > 3) visible = pathNodes;
    const heading = depth === 0 ? '' : `<div class="graph-level-heading"><span>第 ${depth} 层 <small>${state.graphAll ? `${nodes.length} 条思路` : `${visible.length} / ${nodes.length} 条思路`}</small></span>${!state.graphAll && nodes.length > 3 ? `<div class="row-controls"><button data-action="graph-page" data-depth="${depth}" data-index="${Math.max(0, anchor - 1)}" aria-label="第 ${depth} 层上一条思路" ${anchor === 0 ? 'disabled' : ''}>←</button><span>${anchor + 1} / ${nodes.length}</span><button data-action="graph-page" data-depth="${depth}" data-index="${Math.min(nodes.length - 1, anchor + 1)}" aria-label="第 ${depth} 层下一条思路" ${anchor === nodes.length - 1 ? 'disabled' : ''}>→</button></div>` : ''}</div>`;
    return `<section class="graph-level" data-level="${depth}">${heading}<div class="graph-lane" data-depth="${depth}" data-anchor="${id(nodes[anchor]?.id)}"><div class="graph-row">${visible.map(card => graphNodeHTML(card, pathIDs)).join('')}</div></div></section>${depth === current()?.depth ? ghosts : ''}`;
  }).join('');
  const sources = [...state.selected].map(cardByID).filter(Boolean);
  return `<section><div class="overview-head"><div><h1>把思路连起来</h1><p>每层沿着当前思路展开，左右浏览其他方向。</p></div><div class="overview-controls"><button class="secondary" data-action="graph-scope" aria-pressed="${state.graphAll}">${state.graphAll ? '聚焦当前路径' : '显示全部卡片'}</button><button class="secondary" data-action="center-graph">定位当前卡片</button></div></div><div class="graph-scroll" id="graph-scroll"><svg class="graph-canvas" id="graph-canvas" aria-hidden="true" xmlns="http://www.w3.org/2000/svg"></svg><div class="graph-content">${rows}</div></div>${!state.shared ? `<div class="merge-dock">${sources.length ? `<div class="selected-sources" aria-label="准备合并的卡片">${sources.map(card => `<span class="source-chip">${escapeHTML(short(card.title || card.question, 18))}<button data-action="remove-source" data-id="${id(card.id)}" aria-label="移除 ${escapeHTML(card.title || card.question)}">×</button></span>`).join('')}</div>` : ''}<div class="merge-bar"><span>已选择 ${state.selected.size} 张卡片</span><button class="secondary" data-action="clear-selection" ${!state.selected.size ? 'disabled' : ''}>清除</button><button class="send-button" data-action="merge" ${state.selected.size < 2 || state.busy ? 'disabled' : ''}>合并提问 ↗</button></div></div>` : ''}</section>`;
}
function drawGraph() {
  const area = $('#graph-scroll');
  const canvas = $('#graph-canvas');
  if (!area || !canvas) return;
  // SVG keeps deep paths crisp without allocating a huge high-DPI bitmap.
  const width = area.clientWidth;
  const height = $('.graph-content', area).offsetHeight + 64;
  canvas.setAttribute('viewBox', `0 0 ${width} ${height}`);
  canvas.style.width = `${width}px`;
  canvas.style.height = `${height}px`;
  const areaRect = area.getBoundingClientRect();
  const nodes = new Map([...area.querySelectorAll('[data-node]')].map(element => [element.dataset.node, element]));
  const edges = [];
  function connect(parent, child, predicted = false) {
    if (!parent || !child) return;
    const start = parent.getBoundingClientRect();
    const end = child.getBoundingClientRect();
    const x1 = start.left + start.width / 2 - areaRect.left + area.scrollLeft;
    const y1 = start.bottom - areaRect.top + area.scrollTop;
    const x2 = end.left + end.width / 2 - areaRect.left + area.scrollLeft;
    const y2 = end.top - areaRect.top + area.scrollTop;
    edges.push(`<path d="M ${x1} ${y1} C ${x1} ${(y1 + y2) / 2}, ${x2} ${(y1 + y2) / 2}, ${x2} ${y2}" fill="none" stroke="${predicted ? '#ddd3e7' : '#d3c4e7'}" stroke-width="1.5" ${predicted ? 'stroke-dasharray="4 5"' : ''}/>`);
  }
  for (const card of state.session.cards || []) {
    const child = nodes.get(card.id);
    if (child) for (const parentID of card.parent_ids || []) connect(nodes.get(parentID), child);
  }
  for (const ghost of area.querySelectorAll('[data-parent]')) connect(nodes.get(ghost.dataset.parent), ghost, true);
  canvas.innerHTML = edges.join('');
}
function renderCollection() {
  const archived = state.collection === 'archive';
  const cards = state.collectionCards;
  $('#main').innerHTML = `<div class="collection-head"><h1>${archived ? '随时回到过去的思路' : '留下值得再看的答案'}</h1><p>${archived ? '恢复卡片后，就可以从原来的上下文继续。' : '你收藏的卡片，会在这里汇合。'}</p></div>${cards.length ? `<div class="collection-grid">${cards.map(item => `<article class="collection-card" style="${color(item.card.depth)}"><span class="depth-label">${escapeHTML(short(item.session.title, 24))}</span><h2>${escapeHTML(item.card.title || item.card.question)}</h2><p>${escapeHTML(item.card.answer)}</p><div class="actions"><button data-action="collection-open" data-session="${id(item.session.id)}" data-id="${id(item.card.id)}">${archived ? '恢复并继续 ↗' : '回到这张卡片 ↗'}</button>${!archived ? `<button data-action="collection-unfavorite" data-session="${id(item.session.id)}" data-id="${id(item.card.id)}">取消收藏</button>` : ''}</div></article>`).join('')}</div>` : `<div class="empty"><span class="empty-icon">${archived ? '▤' : '☆'}</span>${archived ? '暂时没有归档的卡片' : '还没有收藏<br>在回答卡片上点击 ☆，留下有启发的内容。'}</div>`}`;
}

async function refreshSessions() {
  const result = await api('/api/sessions');
  state.sessions = Array.isArray(result) ? result : result.sessions || [];
  renderSidebar();
}
function acceptSession(session) {
  state.session = session;
  const focused = session.cards?.find(card => card.id === session.focus_id && !card.archived);
  state.focus = focused?.id || session.cards?.filter(card => card.id !== session.root_id && !card.archived).at(-1)?.id || session.root_id;
  state.collection = 'sessions';
  state.compose = false;
  state.selected.clear();
  state.quote = '';
  state.selection = '';
  state.draft = '';
  state.operation = 'deepen';
  state.graphPages = {};
}
async function openSession(sessionID) {
  if (state.busy) return toast('请先等待当前回答完成，或点击停止');
  const navigation = ++navigationVersion;
  const session = await api(`/api/sessions/${encodeURIComponent(sessionID)}`);
  if (navigation !== navigationVersion || state.busy) return;
  state.shared = null;
  history.replaceState({}, '', location.pathname);
  acceptSession(session);
  localStorage.setItem('gitchat.session', session.id);
  $('#sidebar').classList.remove('open');
  $('#sidebar-scrim').classList.remove('open');
  render();
}
async function focusCard(cardID) {
  if (state.busy) return toast('请先等待当前回答完成，或点击停止');
  const card = cardByID(cardID);
  if (!card) return;
  const sessionID = state.session.id;
  const navigation = ++navigationVersion;
  if (!state.shared) {
    await post(`/api/sessions/${sessionID}/focus`, { card_id: cardID });
    const session = await api(`/api/sessions/${sessionID}`);
    if (navigation !== navigationVersion || state.session?.id !== sessionID || state.busy) return;
    state.session = session;
  }
  if (navigation !== navigationVersion || state.session?.id !== sessionID || state.busy) return;
  state.focus = cardID;
  state.selection = '';
  state.compose = false;
  state.quote = '';
  state.operation = 'deepen';
  state.graphPages = {};
  render();
}
function beginCompose(operation = 'deepen') {
  if (state.busy || state.shared) return;
  if (operation === 'merge' && state.selected.size < 2) return toast('至少选择两张卡片，再把思路合在一起');
  navigationVersion++;
  state.operation = operation;
  state.compose = true;
  state.view = 'focus';
  render();
  $('#question')?.focus();
}
async function generate(questionOverride) {
  if (state.busy || state.shared) return;
  const question = (questionOverride ?? $('#question')?.value ?? state.draft).trim();
  if (!question) return toast('先写下你想探索的问题');
  if (state.operation === 'merge' && (state.selected.size < 2 || state.selected.size > 16)) return toast('请选择 2–16 张卡片合并');
  const operation = state.operation;
  const quote = state.quote;
  const selected = [...state.selected];
  const initialFocus = state.focus;
  navigationVersion++;
  state.busy = true;
  state.draft = question;
  state.streamCard = null;
  state.pending = { id: 'pending', question, answer: '', status: 'pending', operation, depth: composeDepth(), parent_ids: composeParents().map(card => card.id) };
  savePendingDraft(question);
  render();
  let sessionID = state.session?.id;
  let terminal = false;
  let reader;
  let created = false;
  let transportFailed = false;
  const controller = new AbortController();
  try {
    if (!state.session) {
      const session = await post('/api/sessions', { app_id: state.config.apps?.[0]?.id || '', title: short(question, 40) });
      acceptSession(session);
      state.draft = question;
      localStorage.setItem('gitchat.session', session.id);
      sessionID = session.id;
      savePendingDraft(question, sessionID);
    }
    const parentIDs = operation === 'merge' ? selected : [initialFocus || state.session.root_id];
    const input = { operation, parent_ids: parentIDs, question, quote, request_id: requestID() };
    state.compose = false;
    state.view = 'focus';
    render();
    window.gitchatPlatform?.assertWorkspace?.();
    const response = await fetch(`/api/sessions/${encodeURIComponent(sessionID)}/chat`, {
      method: 'POST', credentials: 'same-origin', signal: controller.signal, headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', ...(state.config.workspace_id ? { 'X-GitChat-Workspace': state.config.workspace_id } : {}) }, body: JSON.stringify(input),
    });
    if (!response.ok) {
      const error = await response.json().catch(() => ({}));
      throw new Error(error.error || `生成失败（${response.status}）`);
    }
    if (!response.body) throw new Error('浏览器无法读取流式响应');
    reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    function frame(data) {
      const text = data.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n');
      if (!text || text === '[DONE]' || state.session?.id !== sessionID) return;
      const event = JSON.parse(text);
      if (event.card) {
        created = true;
        clearPendingDraft();
        state.pending = null;
        const position = state.session.cards.findIndex(card => card.id === event.card.id);
        if (position === -1) state.session.cards.push(event.card);
        else state.session.cards[position] = event.card;
        state.focus = event.card.id;
        state.streamCard = event.card.id;
      }
      if (event.delta && current()) {
        current().answer = (current().answer || '') + event.delta;
        current().status = 'streaming';
      }
      if (['completed', 'error', 'cancelled'].includes(event.type)) {
        terminal = true;
        if (event.error) toast(event.error);
      }
      if (event.type === 'delta' && current()) updateAnswer(current());
      else render();
    }
    while (true) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
      buffer = buffer.replace(/\r\n/g, '\n');
      let boundary;
      while ((boundary = buffer.indexOf('\n\n')) !== -1) {
        frame(buffer.slice(0, boundary));
        buffer = buffer.slice(boundary + 2);
      }
      if (done) { if (buffer.trim()) frame(buffer); break; }
    }
    if (!terminal) throw new Error('连接在回答完成前中断，正在恢复已保存的卡片');
    if (state.session?.id === sessionID) {
      state.draft = '';
      state.quote = '';
      state.selected.clear();
    }
  } catch (error) {
    transportFailed = true;
    controller.abort();
    await reader?.cancel().catch(() => {});
    if (state.streamCard && sessionID) {
      await post(`/api/sessions/${sessionID}/cards/${state.streamCard}/stop`).catch(() => {});
    }
    toast(error.message);
  } finally {
    reader?.releaseLock();
    if (sessionID) {
      const focus = state.focus;
      try {
        let session = await api(`/api/sessions/${encodeURIComponent(sessionID)}`);
        for (let attempt = 0; transportFailed && attempt < 10 && session.cards.some(card => card.id === state.streamCard && ['pending', 'streaming'].includes(card.status)); attempt++) {
          await new Promise(resolve => setTimeout(resolve, 100));
          session = await api(`/api/sessions/${encodeURIComponent(sessionID)}`);
        }
        if (state.session?.id === sessionID) {
          state.session = session;
          const recovered = session.cards.find(card => card.id === session.focus_id);
          const recoveredRequest = !created && recovered?.question === question && recovered?.id !== initialFocus;
          state.focus = recoveredRequest ? recovered.id : session.cards.some(card => card.id === focus) ? focus : session.focus_id;
          if (transportFailed && !created && !recoveredRequest) {
            state.compose = true;
            state.operation = operation;
            state.quote = quote;
          }
        }
        await refreshSessions();
      } catch (error) { toast(error.message); }
    }
    state.busy = false;
    state.streamCard = null;
    state.pending = null;
    render();
  }
}
async function patch(cardID, changes, sessionID = state.session.id) {
  const card = await api(`/api/sessions/${encodeURIComponent(sessionID)}/cards/${encodeURIComponent(cardID)}`, { method: 'PATCH', body: JSON.stringify(changes) });
  if (sessionID === state.session?.id) {
    const position = state.session.cards.findIndex(item => item.id === cardID);
    if (position !== -1) state.session.cards[position] = card;
  }
  return card;
}
async function collection(name) {
  if (state.busy) return toast('请先等待当前回答完成，或点击停止');
  closeSidebar();
  const navigation = ++navigationVersion;
  state.collection = name;
  if (name === 'sessions') return render();
  $('#main').innerHTML = '<div class="loading">正在整理卡片…</div>';
  await refreshSessions();
  const sessions = [];
  for (let offset = 0; offset < state.sessions.length; offset += 8) {
    sessions.push(...await Promise.all(state.sessions.slice(offset, offset + 8).map(session => api(`/api/sessions/${encodeURIComponent(session.id)}`))));
  }
  if (navigation !== navigationVersion || state.busy) return;
  state.collectionCards = sessions.flatMap(session => (session.cards || []).filter(card => card.id !== session.root_id && (name === 'favorites' ? card.favorite : card.archived)).map(card => ({ card, session })));
  render();
}
async function copyText(text) {
  try { await navigator.clipboard.writeText(text); toast('已复制'); }
  catch (_) { modal('复制内容', `<textarea readonly rows="5">${escapeHTML(text)}</textarea><p>长按或全选文字即可复制。</p>`); }
}
async function shareCard() {
  const result = await post(`/api/sessions/${state.session.id}/share`, { card_id: state.focus });
  const url = new URL(result.url || `/?share=${encodeURIComponent(result.token)}`, location.origin).href;
  modal('分享这条思考路径', `<p>链接包含当前卡片与它的上游思路。对方可以复制这份快照，继续探索。</p><input id="share-url" readonly value="${escapeHTML(url)}" aria-label="分享链接"><div class="dialog-actions"><button class="send-button" data-action="copy-share">复制链接</button></div>`);
}
async function importToken(value) {
  if (state.busy) return toast('请先等待当前回答完成，或点击停止');
  let token = String(value || '').trim();
  if (!token) return toast('请输入分享链接或分享码');
  try { token = new URL(token).searchParams.get('share') || token; } catch (_) { /* Plain capability tokens are supported. */ }
  const navigation = ++navigationVersion;
  const session = await post(`/api/shares/${encodeURIComponent(token)}/import`);
  if (navigation !== navigationVersion || state.busy) return;
  state.shared = null;
  history.replaceState({}, '', location.pathname);
  acceptSession(session);
  closeDialog();
  await refreshSessions();
  localStorage.setItem('gitchat.session', session.id);
  render();
  toast('已复制到你的对话，可以继续探索');
}
async function settings() {
  closeSidebar();
  const [preferences, stats] = await Promise.all([api('/api/preferences'), api('/api/stats')]);
  modal('让回答更贴近你', `<form id="preferences-form"><label for="tone">喜欢的表达方式</label><input id="tone" value="${escapeHTML(preferences.tone || '')}" placeholder="简明、有例子，先说结论" maxlength="300"><label for="interests">感兴趣的话题</label><input id="interests" value="${escapeHTML((preferences.interests || []).join('，'))}" placeholder="旅行，技术，设计" maxlength="1000"><label for="tasks">经常想解决的问题</label><textarea id="tasks" maxlength="1000" placeholder="比较方案、学习新概念…">${escapeHTML((preferences.common_tasks || []).join('，'))}</textarea><label for="learned">从对话中记住的偏好</label><textarea id="learned" maxlength="8000" placeholder="每行一条，可以修改或删除">${escapeHTML((preferences.learned || []).join('\n'))}</textarea><p>这些偏好会用于后续回答。删除某一行即可忘记该条偏好，清空后保存即可删除全部已记住的偏好。</p><div class="stats">已记录 ${Object.values(stats || {}).reduce((sum, count) => sum + (typeof count === 'number' ? count : 0), 0)} 次探索与反馈操作</div><div class="dialog-actions"><button class="send-button" type="submit">保存偏好</button></div></form>`);
}

const actions = {
  'sidebar': () => { $('#sidebar').classList.toggle('open'); $('#sidebar-scrim').classList.toggle('open'); },
  'new': () => {
    if (state.busy) return toast('请先等待当前回答完成，或点击停止');
    navigationVersion++;
    clearPendingDraft();
    closeDialog();
    Object.assign(state, { session: null, focus: null, shared: null, collection: 'sessions', view: 'focus', compose: false, quote: '', draft: '', operation: 'deepen', graphPages: {}, pending: null });
    state.selected.clear();
    history.replaceState({}, '', location.pathname);
    localStorage.removeItem('gitchat.session');
    $('#sidebar').classList.remove('open'); $('#sidebar-scrim').classList.remove('open'); render(); $('#question')?.focus();
  },
  'session': element => openSession(element.dataset.id),
  'view': element => { navigationVersion++; state.view = element.dataset.value; state.collection = 'sessions'; render(); },
  'collection': element => collection(element.dataset.value),
  'starter': element => { state.draft = element.dataset.value; render(); $('#question')?.focus(); },
  'operation': element => { if (state.busy) return; state.draft = $('#question')?.value || state.draft; state.operation = element.dataset.value; render(); $('#question')?.focus(); },
  'compose': element => beginCompose(element.dataset.value),
  'compose-root': () => { if (state.busy || state.shared) return; navigationVersion++; state.focus = state.session.root_id; state.operation = 'deepen'; state.compose = true; state.view = 'focus'; render(); },
  'cancel-compose': () => { state.compose = false; state.quote = ''; state.operation = 'deepen'; render(); },
  'suggestion': element => {
    const suggestion = current()?.suggestions?.[Number(element.dataset.index)];
    if (!suggestion || state.busy) return;
    const question = suggestion.question || suggestion.title;
    if (state.compose) {
      state.draft = question;
      const input = $('#question');
      if (input) { input.value = question; input.focus(); input.setSelectionRange(question.length, question.length); }
      return;
    }
    state.operation = 'deepen';
    return generate(question);
  },
  'focus': element => focusCard(element.dataset.id),
  'graph-focus': async element => { await focusCard(element.dataset.id); if (state.busy) return; state.view = 'focus'; render(); },
  'parent': () => { const parent = current()?.parent_ids?.map(cardByID).find(card => !isRoot(card)); if (parent) return focusCard(parent.id); toast('已经来到这条思路的起点'); },
  'sibling': () => { const nodes = siblings(current()); if (nodes.length < 2) return toast('还没有相邻的思路'); const index = nodes.findIndex(card => card.id === state.focus); return focusCard(nodes[(index - 1 + nodes.length) % nodes.length].id); },
  'select': element => {
    if (state.busy || state.shared) return;
    if (state.selected.has(element.dataset.id)) state.selected.delete(element.dataset.id);
    else if (state.selected.size >= 16) { element.checked = false; return toast('一次最多合并 16 张卡片'); }
    else state.selected.add(element.dataset.id);
    render();
  },
  'remove-source': element => { if (state.busy) return; state.selected.delete(element.dataset.id); render(); },
  'clear-selection': () => { state.selected.clear(); render(); },
  'merge': () => { if (state.selected.size >= 2) beginCompose('merge'); },
  'graph-scope': () => { state.graphAll = !state.graphAll; state.graphPages = {}; render(); },
  'graph-page': element => {
    const depth = element.dataset.depth;
    state.graphPages[depth] = Number(element.dataset.index);
    render();
    requestAnimationFrame(() => {
      const lane = [...document.querySelectorAll('.graph-lane')].find(item => item.dataset.depth === depth);
      const node = lane && [...lane.querySelectorAll('[data-node]')].find(item => item.dataset.node === lane.dataset.anchor);
      if (node) { centerInLane(lane, node); drawGraph(); }
    });
  },
  'center-graph': () => {
    state.graphPages = {};
    render();
    requestAnimationFrame(() => {
      const element = [...document.querySelectorAll('[data-node]')].find(node => node.dataset.node === state.focus);
      if (!element) return;
      centerInLane(element.closest('.graph-lane'), element);
      element.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth', block: 'center', inline: 'nearest' });
      // Programmatic scrolling updates node bounds before WebKit delivers its
      // scroll event. Draw now so connectors never keep the previous positions.
      drawGraph();
    });
  },
  'quote': () => { const text = state.selection.trim(); if (!text) return toast('先在回答中选中一段文字，再点击引用'); state.quote = text.slice(0, 4000); beginCompose('deepen'); },
  'clear-quote': () => { state.quote = ''; render(); },
  'favorite': async () => { await patch(state.focus, { favorite: !current().favorite }); render(); },
  'success': async () => { await patch(state.focus, { success: !current().success }); render(); },
  'stop': async () => { if (!state.streamCard) return toast('请求正在建立，请稍后停止'); await post(`/api/sessions/${state.session.id}/cards/${state.streamCard}/stop`); toast('已请求停止'); },
  'retry': () => {
    const card = current();
    if (!card) return;
    const request = { operation: card.operation === 'merge' ? 'merge' : 'deepen', parent_ids: card.parent_ids || [state.session.root_id], question: card.question, quote: card.quote || '' };
    state.operation = request.operation;
    state.focus = request.parent_ids[0];
    state.selected = new Set(request.parent_ids);
    state.quote = request.quote;
    return generate(request.question);
  },
  'card-menu': () => modal('卡片操作', `<div class="dialog-menu"><button data-action="copy-answer">复制回答</button>${!state.shared ? '<button data-action="copy-node">从这里复制新对话</button><button data-action="share">分享这条思考路径</button><button data-action="archive">归档这张卡片</button>' : '<button data-action="import-current">复制到我的对话</button>'}</div>`),
  'copy-answer': () => { closeDialog(); return copyText(current()?.answer || ''); },
  'copy-node': async () => { if (state.busy) return toast('请先等待当前回答完成，或点击停止'); const navigation = ++navigationVersion; const session = await post(`/api/sessions/${state.session.id}/copy`, { card_id: state.focus }); if (navigation !== navigationVersion || state.busy) return; closeDialog(); acceptSession(session); localStorage.setItem('gitchat.session', session.id); await refreshSessions(); render(); },
  'share': () => shareCard(),
  'copy-share': () => copyText($('#share-url').value),
  'archive': async () => {
    if (state.busy) return toast('请先等待当前回答完成，或点击停止');
    const sessionID = state.session.id;
    await patch(state.focus, { archived: true }, sessionID);
    if (state.session?.id !== sessionID) return;
    closeDialog();
    const next = realCards().find(card => !card.archived);
    await focusCard(next?.id || state.session.root_id);
    state.view = 'overview';
    render();
    toast('已归档，可在归档与历史中恢复');
  },
  'session-menu': () => modal('对话操作', `<div class="dialog-menu"><button data-action="new">开始新的探索</button>${state.session && !state.shared ? '<button data-action="export">导出对话 JSON</button><button class="danger" data-action="delete-dialog">删除当前对话</button>' : ''}<button data-action="import">导入分享</button></div>`),
  'export': async () => {
    closeDialog();
    if (window.__GITCHAT_NATIVE__ && window.gitchatPlatform?.exportSession) {
      const snapshot = await api(`/api/sessions/${state.session.id}/export`);
      window.gitchatPlatform.exportSession(snapshot);
      return;
    }
    const anchor = document.createElement('a');
    anchor.href = `/api/sessions/${state.session.id}/export`;
    anchor.download = `gitchat-${state.session.id}.json`;
    anchor.click();
  },
  'delete-dialog': () => modal('删除这次对话？', '<p>这次对话中的卡片将被删除。已经生成的分享快照会继续保留。</p><div class="dialog-actions"><button class="secondary" data-action="close-dialog">取消</button><button class="send-button" data-action="delete-session">确认删除</button></div>'),
  'delete-session': async () => { if (state.busy) return toast('请先等待当前回答完成，或点击停止'); await api(`/api/sessions/${state.session.id}`, { method: 'DELETE' }); closeDialog(); actions.new(); await refreshSessions(); },
  'import': () => modal('接着别人的思路探索', '<form id="import-form"><label for="import-token">分享链接或分享码</label><input id="import-token" required placeholder="粘贴求知分享链接" autocomplete="off"><div class="dialog-actions"><button class="send-button" type="submit">导入对话</button></div></form>'),
  'import-current': () => importToken(state.shared),
  'collection-open': async element => { if (state.collection === 'archive') await patch(element.dataset.id, { archived: false }, element.dataset.session); await openSession(element.dataset.session); await focusCard(element.dataset.id); state.view = 'focus'; render(); },
  'collection-unfavorite': async element => { await patch(element.dataset.id, { favorite: false }, element.dataset.session); return collection('favorites'); },
  'settings': () => settings(),
  'close-dialog': () => closeDialog(),
  'help': () => modal('按照你的节奏探索', '<div class="help-grid"><div><span>↑ 上滑</span>沿着当前思路深入</div><div><span>← 左滑</span>展开一个平行方向</div><div><span>↓ 下滑</span>返回上一层卡片</div><div><span>→ 右滑</span>切换相邻的思路</div></div><p>在卡片边缘滑动可切换思路，回答区域支持滚动阅读。双指缩放可打开思维图，点击复选框选择多张卡片合并提问。选中回答中的文字后，可点击「引用选中文字」。所有手势都有对应按钮。</p><p>电脑：Enter 发送，Shift + Enter 换行，也可用 ⌘ / Ctrl + Enter 发送。手机：回车换行，点击「开始思考」发送。Esc 返回回答；不在输入框中时，Alt + 方向键可深入、发散或回溯。</p>'),
};

document.addEventListener('click', event => {
  if (event.target.closest('.node-select') && event.target.tagName !== 'INPUT') return;
  const element = event.target.closest('[data-action]');
  if (!element || element.disabled) return;
  if (element.dataset.action === 'select') event.stopPropagation();
  const action = actions[element.dataset.action];
  if (!action) return;
  Promise.resolve().then(() => action(element)).catch(error => toast(error.message));
});
document.addEventListener('submit', event => {
  event.preventDefault();
  let work;
  if (event.target.id === 'chat-form') work = generate();
  if (event.target.id === 'import-form') work = importToken($('#import-token').value);
  if (event.target.id === 'preferences-form') {
    const list = value => value.split(/[,，\n]/).map(item => item.trim()).filter(Boolean);
    const preferences = { tone: $('#tone').value.trim(), interests: list($('#interests').value), common_tasks: list($('#tasks').value), learned: $('#learned').value.split('\n').map(item => item.trim()).filter(Boolean) };
    work = api('/api/preferences', { method: 'PUT', body: JSON.stringify(preferences) }).then(() => { closeDialog(); toast('偏好已保存'); });
  }
  Promise.resolve(work).catch(error => toast(error.message));
});
document.addEventListener('input', event => { if (event.target.id === 'question') state.draft = event.target.value; });
document.addEventListener('keydown', event => {
  const editing = event.target.closest('input,textarea,select,[contenteditable="true"]');
  const touchKeyboard = matchMedia('(pointer: coarse)').matches;
  if (event.target.id === 'question' && event.key === 'Enter' && !event.shiftKey && !event.isComposing && (!touchKeyboard || event.metaKey || event.ctrlKey)) {
    event.preventDefault(); generate().catch(error => toast(error.message));
  }
  if (!$('#dialog').open && !event.isComposing && event.key === 'Escape' && state.compose && !state.busy) { event.preventDefault(); actions['cancel-compose'](); }
  if (!editing && !$('#dialog').open && event.altKey && !state.busy && state.collection === 'sessions') {
    const action = { ArrowUp: () => beginCompose('deepen'), ArrowLeft: () => beginCompose('branch'), ArrowDown: actions.parent, ArrowRight: actions.sibling }[event.key];
    if (action) { event.preventDefault(); Promise.resolve(action()).catch(error => toast(error.message)); }
  }
  const node = event.target.closest('.graph-node[role="button"]');
  if (node && ['Enter', ' '].includes(event.key) && event.target === node) { event.preventDefault(); node.click(); }
});
document.addEventListener('selectionchange', () => {
  const selection = window.getSelection();
  if (selection?.toString() && $('#answer-content')?.contains(selection.anchorNode) && $('#answer-content')?.contains(selection.focusNode)) state.selection = selection.toString();
});
$('#dialog').addEventListener('click', event => { if (event.target === $('#dialog')) closeDialog(); });
window.addEventListener('resize', () => {
  if (state.view !== 'overview') return;
  requestAnimationFrame(() => {
    document.querySelectorAll('.graph-lane').forEach(lane => {
      const anchor = [...lane.querySelectorAll('[data-node]')].find(node => node.dataset.node === lane.dataset.anchor);
      if (anchor) centerInLane(lane, anchor);
    });
    drawGraph();
  });
});

let gesture = null;
function distance(touches) { return Math.hypot(touches[0].clientX - touches[1].clientX, touches[0].clientY - touches[1].clientY); }
$('#main').addEventListener('touchstart', event => {
  if (event.touches.length === 2) {
    event.preventDefault();
    gesture = { pinch: distance(event.touches) };
  }
  else if (event.touches.length === 1 && event.target.closest('.thought-card') && !event.target.closest('button,textarea,input,a')) {
    gesture = { x: event.touches[0].clientX, y: event.touches[0].clientY, start: performance.now(), content: event.target.closest('.answer') };
  }
}, { passive: false });
$('#main').addEventListener('touchmove', event => {
  if (event.touches.length === 2) event.preventDefault();
  if (gesture?.pinch && event.touches.length === 2 && Math.abs(distance(event.touches) - gesture.pinch) > 35) {
    state.view = 'overview'; gesture = null; render();
  }
}, { passive: false });
$('#main').addEventListener('touchend', event => {
  const start = gesture; gesture = null;
  if (!start || start.pinch || state.busy || state.compose || !event.changedTouches.length) return;
  if (window.getSelection()?.toString()) return;
  const dx = event.changedTouches[0].clientX - start.x;
  const dy = event.changedTouches[0].clientY - start.y;
  if (Math.max(Math.abs(dx), Math.abs(dy)) < 75 || performance.now() - start.start > 800) return;
  if (Math.abs(dx) > Math.abs(dy) * 1.4) {
    if (dx < 0) beginCompose('branch'); else Promise.resolve(actions.sibling()).catch(error => toast(error.message));
  } else if (Math.abs(dy) > Math.abs(dx) * 1.4) {
    if (start.content && start.content.scrollHeight > start.content.clientHeight + 5) return;
    if (dy < 0) beginCompose('deepen'); else Promise.resolve(actions.parent()).catch(error => toast(error.message));
  }
}, { passive: true });

async function init() {
  $('#main').innerHTML = '<div class="loading">正在打开你的思考空间…</div>';
  try {
    state.config = await api('/api/config');
    await refreshSessions();
    const share = new URLSearchParams(location.search).get('share');
    if (share) {
      acceptSession(await api(`/api/shares/${encodeURIComponent(share)}`));
      state.shared = share;
    } else {
      const last = localStorage.getItem('gitchat.session') || (window.__GITCHAT_NATIVE__ ? state.sessions[0]?.id : null);
      if (last && state.sessions.some(session => session.id === last)) acceptSession(await api(`/api/sessions/${encodeURIComponent(last)}`));
    }
    await restorePendingDraft();
    render();
  } catch (error) {
    if (!navigator.onLine) { location.replace('/offline.html'); return; }
    $('#main').innerHTML = `<div class="empty"><span class="empty-icon">↻</span>${escapeHTML(error.message)}<br><button class="secondary" onclick="location.reload()">重新连接</button></div>`;
  }
}
init();
