import { Client } from './api.js';

const root = document.querySelector('#app');
const modal = document.querySelector('#modal');
const notices = document.querySelector('#notices');
let client = null, state = null, page = 'sets', pack = null, selected = null;
let sets = [], gifs = [], queue = [], pumping = false, loading = false;
let poll = null, epoch = 0, thumbActive = 0, thumbQueue = [];
const options = { emoji: '🙂', fit: 'speed' };

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attrs)) {
    if (value === undefined || value === null || value === false) continue;
    if (name.startsWith('on')) node.addEventListener(name.slice(2).toLowerCase(), value);
    else if (name === 'class') node.className = value;
    else if (name === 'text') node.textContent = value;
    else if (name in node && !name.startsWith('aria-')) node[name] = value;
    else node.setAttribute(name, value === true ? '' : value);
  }
  node.append(...children.flat().filter(x => x !== null && x !== undefined));
  return node;
}
const text = (tag, content, className) => el(tag, { text: content, class: className });
const button = (label, action, className = '', disabled = false) => el('button', { type: 'button', class: className, text: label, disabled, onclick: action });
const link = (label, href) => el('a', { href, target: '_blank', rel: 'noopener noreferrer', text: label });
function input(name, label, attrs = {}) {
  return el('label', { class: 'field' }, text('span', label), el('input', { name, ...attrs }));
}
function select(name, label, values, current) {
  const field = el('select', { name }, values.map(([value, label]) => el('option', { value, text: label, selected: current === value })));
  return el('label', { class: 'field' }, text('span', label), field);
}
function notify(message) {
  notices.replaceChildren(text('div', message, 'notice'));
  clearTimeout(notify.timer);
  notify.timer = setTimeout(() => notices.replaceChildren(), 6500);
}
function showError(err, target) {
  if (err.name === 'AbortError') return;
  const message = err instanceof TypeError ? '连接服务器失败，请检查网络后重试。' : err.message;
  if (err.status === 401) { disconnect(); notify('令牌不对或已经更换，请重新输入。'); return; }
  if (target?.isConnected) { target.textContent = message; target.hidden = false; }
  else notify(message);
  if (err.status === 409 || err.status === 503) void pollState();
}
async function run(work, errorNode) {
  try { await work(); } catch (err) { showError(err, errorNode); }
}
function busyForm(form, yes) {
  form.querySelectorAll('input,select,button').forEach(node => { node.disabled = yes; });
  form.setAttribute('aria-busy', String(yes));
}
function brand() {
  return el('div', { class: 'brand' }, el('img', { src: '/icon.svg', alt: '', width: 40, height: 40 }), text('span', 'TG Stickers'));
}
function loginFrame(title, description, content) {
  root.replaceChildren(el('main', { class: 'auth-layout' },
    el('section', { class: 'intro' }, brand(), text('h1', '贴纸，\n随手整理。'),
      text('p', '从 GIF 到视频贴纸，把常用的表情放在一起。转换与上传都交给你的服务器。'),
      el('div', { class: 'paper-stack', 'aria-hidden': 'true' }, text('span', '✦', 'paper one'), text('span', '☺', 'paper two'), text('span', 'GIF', 'paper three'))),
    el('section', { class: 'auth-card', 'aria-label': title }, text('h2', title), text('p', description, 'muted'), content)));
}
function tokenScreen() {
  const error = text('p', '', 'error'); error.role = 'alert';
  const tokenField = input('token', '服务器令牌', { type: 'password', required: true, autocomplete: 'off', spellcheck: false });
  const remember = el('input', { type: 'checkbox', name: 'remember' });
  const form = el('form', { class: 'form-stack', onsubmit: event => {
    event.preventDefault();
    busyForm(form, true); error.textContent = '';
    const token = tokenField.querySelector('input').value.trim();
    void run(async () => {
      const c = new Client(token);
      try {
        const s = await c.request('/state');
        client?.close(); client = c; state = s;
        try { if (remember.checked) sessionStorage.setItem('tg-stickers-token', token); else sessionStorage.removeItem('tg-stickers-token'); } catch { /* 存储被禁用时继续使用当前页面 */ }
        epoch++;
        await enterState();
        startPolling();
      } catch (err) { c.close(); throw err; }
    }, error).finally(() => busyForm(form, false));
  } }, tokenField,
    el('label', { class: 'check' }, remember, '在本标签页记住令牌'), error,
    el('button', { type: 'submit', class: 'primary', text: '打开管理面板' }),
    el('details', {}, text('summary', '在哪里查看令牌？'), text('p', '在服务器的 compose.yaml 所在目录运行：'), text('code', 'docker compose exec tg-stickers tg-stickers token')));
  loginFrame('连接你的服务器', '使用部署时生成的令牌进入。Telegram 账号会在下一步登录。', form);
}
function disconnect() {
  epoch++; client?.close(); client = null; state = null; pack = null; selected = null;
  sets = []; gifs = []; queue = []; pumping = false; loading = false; thumbQueue = [];
  clearInterval(poll); modal.close();
  try { sessionStorage.removeItem('tg-stickers-token'); } catch { /* 没有可用存储 */ }
  tokenScreen();
}
function startPolling() { clearInterval(poll); poll = setInterval(() => void pollState(), 3000); }
let polling = false;
async function pollState() {
  if (!client || polling) return;
  // 已登录时一分钟检查一次，避免与管理操作争抢连接。
  if (state?.step === 'ready' && Date.now() - (pollState.last || 0) < 60000) return;
  const c = client, gen = epoch;
  polling = true; pollState.last = Date.now();
  try {
    const next = await c.request('/state');
    if (c !== client || gen !== epoch) return;
    const changed = next.step !== state?.step || next.error !== state?.error;
    state = next;
    if (changed) await enterState();
  } catch (err) { if (gen === epoch) showError(err); }
  finally { polling = false; }
}
async function enterState() {
  if (state.step === 'ready') { renderShell(); await loadPage(); }
  else { modal.close(); pack = null; selected = null; renderTelegramLogin(); }
}
function renderTelegramLogin() {
  const step = state.step;
  const error = text('p', '', 'error'); error.role = 'alert';
  const form = el('form', { class: 'form-stack' });
  let title, desc, fields, endpoint, method = 'POST', toBody;
  if (step === 'credentials') {
    title = '设置 Telegram 应用'; desc = '填写你在 my.telegram.org 申请的 api_id 和 api_hash。';
    fields = [link('打开 API development tools', 'https://my.telegram.org/apps'), input('api_id', 'api_id', { required: true, type: 'number', min: 1, inputMode: 'numeric' }),
      input('api_hash', 'api_hash', { required: true, minLength: 32, maxLength: 32, autocomplete: 'off', spellcheck: false })];
    endpoint = '/credentials'; method = 'PUT';
    toBody = data => ({ api_id: Number(data.get('api_id')), api_hash: data.get('api_hash').trim() });
  } else if (step === 'phone') {
    title = '登录 Telegram'; desc = '登录你创建贴纸包时使用的账号。手机号需要包含国家码。';
    fields = [input('phone', '手机号', { type: 'tel', required: true, placeholder: '+86 …', autocomplete: 'tel' })];
    endpoint = '/login/phone'; toBody = data => ({ phone: data.get('phone') });
  } else if (step === 'code') {
    title = '输入验证码'; desc = `验证码已发到${state.code_via || 'Telegram'}。`;
    fields = [input('code', '验证码', { required: true, autocomplete: 'one-time-code', inputMode: 'numeric' })];
    endpoint = '/login/code'; toBody = data => ({ code: data.get('code').trim() });
  } else if (step === 'password') {
    title = '两步验证'; desc = '你的账号开启了两步验证，请输入 Telegram 密码。';
    fields = [input('password', '两步验证密码', { type: 'password', required: true, autocomplete: 'current-password' })];
    if (state.password_hint) fields.push(text('p', `密码提示：${state.password_hint}`, 'muted'));
    endpoint = '/login/password'; toBody = data => ({ password: data.get('password') });
  } else {
    const content = el('div', { class: 'form-stack' }, text('p', '正在连接 Telegram…', 'working'),
      state.error ? text('p', state.error, 'error') : null,
      text('p', '连不上时检查服务器网络，或在服务端配置 TG_STICKERS_PROXY。', 'muted'),
      button('断开服务器', disconnect));
    loginFrame('连接 Telegram', '连接成功后将自动继续。', content); return;
  }
  form.append(...fields, error, el('button', { type: 'submit', class: 'primary', text: step === 'credentials' ? '保存并连接' : step === 'phone' ? '发送验证码' : '登录' }));
  form.addEventListener('submit', event => {
    event.preventDefault(); const body = toBody(new FormData(form)); busyForm(form, true); error.textContent = '';
    const c = client;
    void run(async () => {
      const next = await c.request(endpoint, method, body);
      if (c !== client) return;
      state = next; form.reset(); await enterState();
    }, error).finally(() => busyForm(form, false));
  });
  if (step === 'code' || step === 'password') form.append(button('换个手机号 / 重新获取验证码', () => void run(async () => {
    const c = client; const next = await c.request('/login', 'DELETE'); if (c !== client) return; state = next; renderTelegramLogin();
  }, error), 'quiet'));
  form.append(button('断开服务器', disconnect, 'quiet'));
  loginFrame(title, desc, form);
}

function renderShell() {
  const nav = el('nav', { 'aria-label': '管理分类' }, [['sets', '贴纸包'], ['gifs', 'GIF 收藏'], ['account', '账号']].map(([id, label]) =>
    button(label, () => { page = id; pack = null; selected = null; renderShell(); void loadPage(); }, page === id ? 'nav-item active' : 'nav-item')));
  nav.querySelector('.active')?.setAttribute('aria-current', 'page');
  root.replaceChildren(el('div', { class: 'workspace' },
    el('aside', { class: 'sidebar' }, brand(), nav,
      el('div', { class: 'connection' }, text('strong', state.user?.name || 'Telegram 已登录'), text('span', location.host), button('断开服务器', disconnect, 'quiet'))),
    el('main', { class: 'main' }, el('div', { id: 'content', 'aria-live': 'polite' })),
    el('section', { id: 'queue', class: 'queue hidden', 'aria-label': '上传队列' })));
  renderQueue();
}
const content = () => document.querySelector('#content');
function header(title, description, actions = []) {
  return el('header', { class: 'page-header' }, el('div', {}, text('h1', title), text('p', description, 'muted')), el('div', { class: 'actions' }, actions));
}
async function loadPage() {
  if (!client || state?.step !== 'ready') return;
  if (page === 'account') { renderAccount(); return; }
  const c = client, requestedPage = page, name = pack?.short_name, gen = epoch;
  loading = true; renderPage();
  try {
    const data = await c.request(page === 'gifs' ? '/gifs' : name ? `/sets/${encodeURIComponent(name)}` : '/sets');
    if (gen !== epoch || page !== requestedPage || pack?.short_name !== name) return;
    if (page === 'gifs') gifs = data;
    else if (name) pack = data;
    else sets = data;
  } catch (err) { if (gen === epoch) showError(err); }
  finally { if (gen === epoch && page === requestedPage && pack?.short_name === name) { loading = false; renderPage(); } }
}
function renderPage() {
  if (!content()) return;
  if (page === 'account') renderAccount();
  else if (page === 'gifs') renderGIFs();
  else if (pack) renderPack();
  else renderSets();
}
function empty(title, desc, action) {
  return el('div', { class: 'empty' }, el('img', { src: '/icon.svg', width: 64, height: 64, alt: '' }), text('h2', title), text('p', desc, 'muted'), action);
}
function renderSets() {
  const box = content(); if (!box) return;
  box.replaceChildren(header('我的贴纸包', '你账号创建的贴纸包，打开即可整理或添加。', [button('刷新', () => void loadPage(), '', loading), button('新建贴纸包', createPack, 'primary')]),
    ...(!state.ffmpeg ? [text('p', '服务器没有找到 ffmpeg，暂时无法转换上传的文件。请安装 ffmpeg，或使用自带 ffmpeg 的 Docker 镜像。', 'warning')] : []),
    ...(loading ? [text('p', '正在读取贴纸包…', 'working')] : []));
  if (!loading && !sets.length) { box.append(empty('从第一张贴纸开始', '选择图片或 GIF，创建你自己的贴纸包。', button('新建贴纸包', createPack, 'primary'))); return; }
  box.append(el('div', { class: 'pack-grid' }, sets.map(s => {
    const card = button('', () => { pack = { ...s, stickers: [] }; selected = null; void loadPage(); }, 'pack-card');
    card.append(thumbnail('stickers', s.cover, s.title), text('strong', s.title), text('span', `${s.count} 张${s.emojis ? ' · 自定义表情' : s.masks ? ' · 面具' : ''}${s.archived ? ' · 已归档' : ''}`, 'muted'));
    return card;
  })));
}
function fileOptions(includeEmoji = true) {
  const group = el('div', { class: 'upload-options' });
  if (includeEmoji) {
    const emoji = input('emoji', '这批贴纸的表情', { value: options.emoji, placeholder: '🙂', maxLength: 32 });
    emoji.querySelector('input').addEventListener('input', e => { options.emoji = e.target.value; }); group.append(emoji);
  }
  const fit = select('fit', '超过 3 秒的动图', [['speed', '加速到 3 秒，保留整段'], ['trim', '只取前 3 秒']], options.fit);
  fit.querySelector('select').addEventListener('change', e => { options.fit = e.target.value; });
  group.append(fit); return group;
}
function uploadZone(target) {
  const gif = target === '';
  const picker = el('input', { type: 'file', multiple: true, class: 'file-input', accept: gif ? '.gif,.webp,.webm,.mp4,.mov' : '.gif,.png,.jpg,.jpeg,.webp,.webm,.mp4,.mov', 'aria-label': gif ? '选择动图' : '选择贴纸文件' });
  const zone = el('section', { class: 'upload-zone', 'aria-label': '文件上传' },
    el('div', { class: 'upload-heading' }, el('div', {}, text('h2', gif ? '添加到 GIF 收藏' : '添加贴纸'),
      text('p', gif ? '拖入动图，自动转换并存进 Telegram 的 GIF 收藏。' : '拖入图片或 GIF，自动转换成合适的贴纸格式。支持一次选择多个文件。', 'muted')),
      button('选择文件', () => picker.click(), 'primary', !state.ffmpeg)),
    gif ? null : fileOptions(), picker,
    text('p', '单个文件最大 50 MB。上传与转换过程中请保持本页打开。', 'hint'));
  picker.addEventListener('change', () => { enqueue([...picker.files], target); picker.value = ''; });
  zone.addEventListener('dragover', e => { e.preventDefault(); zone.classList.add('dragging'); });
  zone.addEventListener('dragleave', e => { if (!zone.contains(e.relatedTarget)) zone.classList.remove('dragging'); });
  zone.addEventListener('drop', e => { e.preventDefault(); zone.classList.remove('dragging'); enqueue([...e.dataTransfer.files], target); });
  return zone;
}
function renderPack() {
  const box = content(); if (!box) return;
  const s = pack, ordinary = !s.emojis && !s.masks;
  const actions = [button('刷新', () => void loadPage(), '', loading), button('改名', renamePack), button('删除贴纸包', deletePack, 'danger')];
  const url = `https://t.me/${s.emojis ? 'addemoji' : 'addstickers'}/${encodeURIComponent(s.short_name)}`;
  box.replaceChildren(button('‹ 全部贴纸包', () => { pack = null; selected = null; void loadPage(); }, 'back'),
    header(s.title, `${s.stickers.length || s.count} 张贴纸`, actions),
    link(`t.me/${s.emojis ? 'addemoji' : 'addstickers'}/${s.short_name}`, url),
    ...(loading ? [text('p', '正在读取贴纸…', 'working')] : []),
    ordinary ? uploadZone(s.short_name) : text('p', '自定义表情包和面具包支持整理；上传目前支持普通贴纸包。', 'warning'));
  const index = s.stickers.findIndex(st => String(st.id) === selected);
  if (index >= 0) box.append(stickerTools(s.stickers[index], index));
  const grid = el('div', { class: 'sticker-grid' }, s.stickers.map((st, i) => {
    const id = String(st.id), card = button('', () => { selected = selected === id ? null : id; renderPack(); }, `sticker-cell${selected === id ? ' selected' : ''}`);
    card.setAttribute('aria-label', `第 ${i + 1} 张贴纸 ${st.emoji || ''}`); card.setAttribute('aria-pressed', String(selected === id));
    card.append(thumbnail('stickers', st.id, st.emoji || '贴纸'), el('span', { class: 'sticker-caption' }, text('span', st.emoji || '🙂'), text('small', st.kind === 'video' ? '视频' : st.kind === 'animated' ? '动画' : '静态')));
    return card;
  }));
  if (s.stickers.length) box.append(text('p', '选择一张贴纸，修改表情、移动位置或删除。', 'hint'), grid);
  else if (!loading) box.append(text('p', '贴纸包里还没有贴纸。', 'muted'));
}
function stickerTools(st, index) {
  const field = el('input', { value: st.emoji, maxLength: 32, 'aria-label': '贴纸表情', required: true });
  const bar = el('form', { class: 'selection-bar', onsubmit: event => {
    event.preventDefault(); if (!field.value.trim()) return;
    void changeSticker(st.id, { emoji: field.value.trim() }, bar);
  } }, text('strong', `第 ${index + 1} 张`), field, el('button', { type: 'submit', text: '改表情' }),
    button('移到最前', () => void changeSticker(st.id, { position: 0 }, bar), '', index === 0),
    button('前移', () => void changeSticker(st.id, { position: index - 1 }, bar), '', index === 0),
    button('后移', () => void changeSticker(st.id, { position: index + 1 }, bar), '', index === pack.stickers.length - 1),
    button('移到最后', () => void changeSticker(st.id, { position: pack.stickers.length - 1 }, bar), '', index === pack.stickers.length - 1),
    button('删除', () => confirmAction('删除这张贴纸？', pack.stickers.length === 1 ? '这是最后一张贴纸。Telegram 可能会同时删除这个贴纸包。' : '这张贴纸会从包中删除，已经发送的消息不会改变。', '删除贴纸', async () => {
      const c = client, name = pack.short_name; const result = await c.request(`/stickers/${st.id}`, 'DELETE');
      if (c !== client) return; applyPack(result, name); notify('贴纸已删除');
    }), 'danger'));
  return bar;
}
async function changeSticker(id, patch, bar) {
  const c = client, name = pack.short_name;
  busyForm(bar, true);
  await run(async () => { const result = await c.request(`/stickers/${id}`, 'PATCH', patch); if (c === client) applyPack(result, name); });
  if (bar.isConnected) busyForm(bar, false);
}
function applyPack(result, name) {
  sets = sets.map(s => s.short_name === result.short_name ? result : s);
  if (pack?.short_name === name) {
    pack = result;
    if (!result.stickers.some(s => String(s.id) === selected)) selected = null;
    if (page === 'sets') renderPack();
  }
}
function renderGIFs() {
  const box = content(); if (!box) return;
  box.replaceChildren(header('GIF 收藏', '管理 Telegram「GIF」标签页里的动图。', [button('刷新', () => void loadPage(), '', loading)]), uploadZone(''));
  if (loading) box.append(text('p', '正在读取 GIF 收藏…', 'working'));
  if (!loading && !gifs.length) box.append(empty('收藏里还没有动图', '把 GIF 拖到上面的区域，就能在 Telegram 里使用。'));
  box.append(el('div', { class: 'gif-grid' }, gifs.map(g => el('article', { class: 'gif-cell' }, thumbnail('gifs', g.id, 'GIF 缩略图'),
    el('div', { class: 'gif-caption' }, text('span', g.w ? `${g.w} × ${g.h}` : 'GIF'), button('移除', () => confirmAction('从 GIF 收藏移除？', '此动图将从你账号的 GIF 收藏中移除。', '移除', async () => {
      const c = client; const result = await c.request(`/gifs/${g.id}`, 'DELETE'); if (c !== client) return; gifs = result; if (page === 'gifs') renderGIFs(); notify('已从 GIF 收藏移除');
    }), 'quiet danger'))))));
}
function renderAccount() {
  const u = state.user;
  content()?.replaceChildren(header('账号与服务器', '管理当前 Telegram 登录和面板连接。'),
    el('section', { class: 'settings-section' }, text('h2', 'Telegram 账号'), text('strong', u?.name || ''), text('p', [u?.username ? `@${u.username}` : '', u?.phone ? `+${u.phone}` : '', u?.id ? `ID ${u.id}` : ''].filter(Boolean).join(' · '), 'muted'),
      button('退出 Telegram 登录', () => confirmAction('退出 Telegram 登录？', '注销服务器保存的登录会话。贴纸包会保留，以后可以重新登录。', '退出登录', async () => {
        const c = client; const next = await c.request('/logout', 'POST'); if (c !== client) return; state = next; sets = []; gifs = []; queue = []; pack = null; renderTelegramLogin();
      }), 'danger')),
    el('section', { class: 'settings-section' }, text('h2', '服务器'), text('p', location.origin), text('p', `服务端 ${state.version} · ${state.ffmpeg ? `ffmpeg ${state.ffmpeg}` : '未找到 ffmpeg'}`, 'muted'),
      text('p', '会话保存在服务器的数据目录中。浏览器关闭后 Telegram 会话仍会保留。', 'hint'), button('断开服务器', disconnect)));
}

// 用受保护的 fetch 获取图片，避免把令牌写进 URL。最多四个请求并发。
function thumbnail(type, id, alt) {
  const node = el('div', { class: 'thumbnail', 'aria-label': alt });
  if (!id || String(id) === '0') { node.append(text('span', '✦', 'thumb-placeholder')); return node; }
  node.append(text('span', '…', 'thumb-placeholder'));
  const c = client;
  thumbQueue.push({ type, id, node, client: c }); queueMicrotask(pumpThumbs);
  return node;
}
function pumpThumbs() {
  while (thumbActive < 4 && thumbQueue.length) {
    const job = thumbQueue.shift();
    if (job.client !== client || !job.node.isConnected) continue;
    thumbActive++;
    void job.client.thumb(job.type, job.id).then(url => {
      if (job.client === client) job.node.replaceChildren(el('img', { src: url, alt: '', loading: 'lazy' }));
    }).catch(() => job.node.replaceChildren(text('span', '无预览', 'thumb-placeholder small'))).finally(() => { thumbActive--; pumpThumbs(); });
  }
}

function openModal(title, children) {
  modal.replaceChildren(el('div', { class: 'dialog-header' }, text('h2', title), button('关闭', () => modal.close(), 'quiet')),
    ...children);
  modal.querySelector('h2').id = 'modal-title';
  if (!modal.open) modal.showModal();
}
function confirmAction(title, description, label, action) {
  const error = text('p', '', 'error'); error.role = 'alert';
  const controls = el('div', { class: 'actions' });
  const ok = button(label, () => {
    controls.querySelectorAll('button').forEach(b => { b.disabled = true; });
    void run(async () => { await action(); modal.close(); }, error).finally(() => controls.querySelectorAll('button').forEach(b => { b.disabled = false; }));
  }, 'danger solid');
  controls.append(button('取消', () => modal.close()), ok);
  openModal(title, [text('p', description, 'muted'), error, controls]);
}
function renamePack() {
  const name = pack.short_name;
  const error = text('p', '', 'error'); error.role = 'alert';
  const form = el('form', { class: 'form-stack', onsubmit: event => {
    event.preventDefault(); const title = new FormData(form).get('title').trim(); if (!title) return;
    busyForm(form, true); const c = client;
    void run(async () => { const result = await c.request(`/sets/${encodeURIComponent(name)}`, 'PATCH', { title }); if (c !== client) return; applyPack(result, name); modal.close(); notify('贴纸包已改名'); }, error).finally(() => busyForm(form, false));
  } }, input('title', '贴纸包名字', { value: pack.title, maxLength: 64, required: true }), text('p', '只修改显示名字，分享链接保持不变。', 'hint'), error, el('button', { type: 'submit', class: 'primary', text: '保存名字' }));
  openModal('修改贴纸包名字', [form]);
}
function deletePack() {
  const name = pack.short_name, title = pack.title;
  confirmAction('删除整个贴纸包？', `「${title}」将从 Telegram 删除，其他人也无法再添加。此操作无法撤销。`, '删除贴纸包', async () => {
    const c = client; await c.request(`/sets/${encodeURIComponent(name)}`, 'DELETE'); if (c !== client) return;
    sets = sets.filter(s => s.short_name !== name); if (pack?.short_name === name) pack = null; if (page === 'sets') renderPage(); notify('贴纸包已删除');
  });
}
function createPack() {
  const error = text('p', '', 'error'); error.role = 'alert';
  const checkResult = text('p', '英文字母开头，可用字母、数字和下划线，不能有连续下划线。', 'hint');
  const short = input('short_name', '链接名（创建后不能更改）', { required: true, maxLength: 64, pattern: '[A-Za-z][A-Za-z0-9_]*', placeholder: 'my_stickers', autocomplete: 'off', spellcheck: false });
  const check = button('检查链接名', () => {
    const name = short.querySelector('input').value.trim(); if (!name) return;
    check.disabled = true; const c = client;
    void run(async () => {
      const result = await c.request(`/short-names/${encodeURIComponent(name)}`);
      if (c !== client || short.querySelector('input').value.trim() !== name) return;
      checkResult.textContent = result.available ? '这个链接名可以使用。' : result.reason || '链接名已被使用。';
    }, error).finally(() => { check.disabled = false; });
  });
  let files = [];
  const fileList = text('p', '至少选择一张图片或动图。第一张用来创建，其余会排队添加。', 'hint');
  const picker = el('input', { type: 'file', name: 'files', multiple: true, required: true, accept: '.gif,.png,.jpg,.jpeg,.webp,.webm,.mp4,.mov', 'aria-label': '选择贴纸文件' });
  picker.addEventListener('change', () => { files = [...picker.files]; fileList.textContent = files.length ? files.map(f => f.name).join('、') : '至少选择一个文件。'; });
  const form = el('form', { class: 'form-stack', onsubmit: event => {
    event.preventDefault(); const data = new FormData(form); const valid = validateFiles(files);
    if (!valid.length || valid.length !== files.length) return;
    busyForm(form, true);
    const c = client, gen = epoch, first = valid[0], rest = valid.slice(1);
    const uploadOptions = { emoji: options.emoji, fit: options.fit };
    const body = new FormData(); body.set('title', data.get('title')); body.set('short_name', data.get('short_name'));
    body.set('emoji', uploadOptions.emoji); body.set('fit', uploadOptions.fit); body.set('file', first);
    fileList.textContent = '正在转换并上传第一张贴纸，请保持本页打开…';
    void run(async () => {
      const result = await c.request('/sets', 'POST', body); if (gen !== epoch) return;
      pack = result; selected = null; page = 'sets'; sets.unshift(result); modal.close(); renderShell(); renderPack();
      enqueue(rest, result.short_name, uploadOptions); notify('贴纸包已创建');
    }, error).finally(() => { busyForm(form, false); fileList.textContent = files.map(f => f.name).join('、'); });
  } }, input('title', '贴纸包名字', { required: true, maxLength: 64, placeholder: '我的表情' }), short, check, checkResult,
    fileOptions(), inputFileLabel(picker), fileList, error, el('button', { type: 'submit', class: 'primary', text: '创建贴纸包' }));
  openModal('新建贴纸包', [form]);
}
function inputFileLabel(picker) { return el('label', { class: 'field' }, text('span', '图片或动图（可多选）'), picker); }

function validateFiles(files) {
  const limit = Number(state?.limits?.max_upload_bytes) || 50 * 1024 * 1024;
  return files.filter(file => {
    if (file.size > limit) { notify(`${file.name} 超过 ${Math.floor(limit / 1024 / 1024)} MB，未加入队列。`); return false; }
    if (!file.size) { notify(`${file.name} 是空文件，未加入队列。`); return false; }
    return true;
  });
}
function enqueue(files, target, uploadOptions = options) {
  if (!state?.ffmpeg) { notify('服务器未找到 ffmpeg，无法转换文件。'); return; }
  for (const file of validateFiles(files)) queue.push({ file, target, emoji: uploadOptions.emoji, fit: uploadOptions.fit, status: 'waiting', error: '' });
  renderQueue(); void pump();
}
async function pump() {
  if (pumping || !client) return;
  const gen = epoch, c = client;
  pumping = true;
  try {
    for (let job; (job = queue.find(j => j.status === 'waiting'));) {
      if (gen !== epoch || state?.step !== 'ready') break;
      job.status = 'running'; renderQueue();
      const body = new FormData(); body.set('emoji', job.emoji); body.set('fit', job.fit); body.set('file', job.file);
      try {
        const result = await c.request(job.target ? `/sets/${encodeURIComponent(job.target)}/stickers` : '/gifs', 'POST', body);
        if (gen !== epoch) return;
        job.status = 'done';
        // 成功后释放浏览器持有的源文件；失败项保留以便手动重试。
        job.name = job.file.name; job.file = null;
        if (job.target) applyPack(result, job.target);
        else { gifs = result; if (page === 'gifs') renderGIFs(); }
      } catch (err) {
        if (gen !== epoch || err.name === 'AbortError') return;
        job.status = 'failed'; job.error = err.message;
        if (err.status === 401 || err.status === 409 || err.status === 429) {
          showError(err); break;
        }
      }
      renderQueue();
    }
  } finally { if (gen === epoch) { pumping = false; renderQueue(); } }
}
function renderQueue() {
  const area = document.querySelector('#queue'); if (!area) return;
  area.classList.toggle('hidden', queue.length === 0); if (!queue.length) return;
  const done = queue.filter(j => j.status === 'done').length;
  const failed = queue.filter(j => j.status === 'failed').length;
  area.replaceChildren(el('div', { class: 'queue-header' }, text('strong', `上传 ${done} / ${queue.length}${failed ? `，${failed} 个失败` : ''}`),
    text('span', '转换与上传由服务器执行', 'hint'),
    button('清除已完成', () => { queue = queue.filter(j => j.status !== 'done'); renderQueue(); }, 'quiet', done === 0)),
    el('ul', { class: 'queue-list' }, queue.map(job => {
      const status = { waiting: '等待中', running: '正在上传 / 转换', done: '已完成', failed: '失败' }[job.status];
      return el('li', {}, el('span', { class: `job-status ${job.status}`, text: status }),
        text('strong', job.file?.name || job.name), text('span', job.target || 'GIF 收藏', 'hint'),
        job.error ? text('span', job.error, 'error') : null,
        job.status === 'failed' ? button('重试', () => { job.status = 'waiting'; job.error = ''; void pump(); renderQueue(); }, 'quiet') : null,
        job.status === 'waiting' || job.status === 'failed' ? button('移出队列', () => { queue = queue.filter(j => j !== job); renderQueue(); }, 'quiet') : null);
    })));
}
window.addEventListener('beforeunload', event => {
  if (pumping || queue.some(j => j.status === 'waiting')) { event.preventDefault(); event.returnValue = ''; }
});
// 避免把文件拖到页面其他位置时浏览器直接打开文件、丢掉上传队列。
window.addEventListener('dragover', event => event.preventDefault());
window.addEventListener('drop', event => event.preventDefault());

tokenScreen();
let saved;
try { saved = sessionStorage.getItem('tg-stickers-token'); } catch { /* 存储被禁用 */ }
if (saved) {
  const c = new Client(saved);
  void run(async () => {
    try { const s = await c.request('/state'); if (client) { c.close(); return; } client = c; state = s; epoch++; await enterState(); startPolling(); }
    catch (err) { c.close(); throw err; }
  });
}
