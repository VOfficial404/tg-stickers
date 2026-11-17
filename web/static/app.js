import { Client } from './api.js';
import { codeDelivery, resendStatus } from './login.js';
import { GIF_SIZES, QUALITIES, RESIZE_MODES, STICKER_SIZES, appendMedia, defaultMedia, describeMedia, snapshotMedia } from './media.js';
import { button, el, input, link, select, text } from './dom.js';
import { openEditor } from './editor.js';

const root = document.querySelector('#app');
const modal = document.querySelector('#modal');
const notices = document.querySelector('#notices');
let client = null, state = null, page = 'sets', pack = null, selected = null;
let sets = [], gifs = [], queue = [], pumping = false, loading = false;
let poll = null, epoch = 0, thumbActive = 0, thumbQueue = [];
// 上传区表单的当前值（原始字符串）。排队时另存校验后的快照，之后修改这里不影响已排队文件。
const media = { sticker: defaultMedia('sticker'), gif: defaultMedia('gif') }, mediaOpen = { sticker: false, gif: false };
let loginBusy = false, qrImageURL = '', qrImageKey = '', qrPendingKey = '';

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
  if (err.status === 409 || err.status === 503) void pollState(true);
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
  clearQRImage(); loginBusy = false;
  sets = []; gifs = []; queue = []; pumping = false; loading = false; thumbQueue = [];
  clearInterval(poll); modal.close();
  try { sessionStorage.removeItem('tg-stickers-token'); } catch { /* 没有可用存储 */ }
  tokenScreen();
}
function startPolling() { clearInterval(poll); poll = setInterval(() => { updateLoginTimers(); void pollState(); }, 1000); }
let polling = false;
async function pollState(force = false) {
  if (!client || polling) return;
  // 已登录时一分钟检查一次，避免与管理操作争抢连接。
  if (!force && state?.step === 'ready' && Date.now() - (pollState.last || 0) < 60000) return;
  const c = client, gen = epoch;
  polling = true; pollState.last = Date.now();
  try {
    const next = await c.request('/state');
    if (c !== client || gen !== epoch) return;
    const changed = next.step !== state?.step || next.error !== state?.error || next.qr?.url !== state?.qr?.url
      || next.code_notice !== state?.code_notice || next.code_via !== state?.code_via
      || next.code_sent !== state?.code_sent || next.code_next !== state?.code_next
      || next.code_retry_at !== state?.code_retry_at || next.code_can_resend !== state?.code_can_resend;
    state = next;
    if (changed) await enterState();
  } catch (err) { if (gen === epoch) showError(err); }
  finally { polling = false; }
}
async function enterState() {
  if (state.step !== 'qr') clearQRImage();
  if (state.step === 'ready') { renderShell(); await loadPage(); }
  else { modal.close(); pack = null; selected = null; renderTelegramLogin(); }
}
function renderTelegramLogin() {
  const step = state.step;
  const error = text('p', '', 'error'); error.role = 'alert';
  if (step === 'qr') { renderQRLogin(error); return; }
  const form = el('form', { class: 'form-stack' });
  let title, desc, fields, endpoint, method = 'POST', toBody;
  if (step === 'credentials') {
    title = '设置 Telegram 应用'; desc = '填写你在 my.telegram.org 申请的 api_id 和 api_hash。';
    fields = [link('打开 API development tools', 'https://my.telegram.org/apps'), input('api_id', 'api_id', { required: true, type: 'number', min: 1, inputMode: 'numeric' }),
      input('api_hash', 'api_hash', { required: true, minLength: 32, maxLength: 32, autocomplete: 'off', spellcheck: false })];
    endpoint = '/credentials'; method = 'PUT';
    toBody = data => ({ api_id: Number(data.get('api_id')), api_hash: data.get('api_hash').trim() });
  } else if (step === 'phone') {
    title = '登录 Telegram'; desc = '已有手机 Telegram 登录？可以直接扫码，不需要等待短信。';
    fields = [button('使用手机 Telegram 扫码登录', () => void loginAction('/login/qr', error), 'primary'),
      text('p', '或使用手机号获取验证码。发送渠道由 Telegram 决定，第三方程序不能强制发送短信。', 'hint'),
      input('phone', '国际区号 + 手机号', { type: 'tel', required: true, placeholder: '美国：+1 加 10 位手机号', autocomplete: 'tel' })];
    endpoint = '/login/phone'; toBody = data => ({ phone: data.get('phone') });
  } else if (step === 'code') {
    title = state.code_sent ? '输入验证码' : '当前渠道需要额外验证'; desc = codeDelivery(state);
    fields = [];
    if (state.code_notice) fields.push(text('p', state.code_notice, 'warning'));
    if (state.code_sent) fields.push(input('code', '验证码（也可能是单词或短语）', { required: true, autocomplete: 'one-time-code' }));
    endpoint = '/login/code'; toBody = data => ({ code: String(data.get('code') || '').trim() });
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
  form.append(...fields, error);
  if (step !== 'code' || state.code_sent) form.append(el('button', { type: 'submit', class: 'primary', text: step === 'credentials' ? '保存并连接' : step === 'phone' ? '发送验证码' : '登录' }));
  form.addEventListener('submit', event => {
    event.preventDefault();
    if (step === 'code' && !state.code_sent) return;
    const body = toBody(new FormData(form));
    void loginAction(endpoint, error, method, body);
  });
  if (step === 'code') {
    const resend = button('', () => void loginAction('/login/resend', error)); resend.id = 'resend-code';
    form.append(resend, button('收不到？改用扫码登录', () => void loginAction('/login/qr', error), 'quiet'));
  }
  if (step === 'code' || step === 'password') form.append(button('换个手机号 / 重新开始', () => void loginAction('/login', error, 'DELETE'), 'quiet'));
  form.append(button('断开服务器', disconnect, 'quiet'));
  loginFrame(title, desc, form);
  busyForm(form, loginBusy); updateLoginTimers();
}

async function loginAction(endpoint, error, method = 'POST', body) {
  if (loginBusy || !client) return;
  const c = client, gen = epoch;
  const form = root.querySelector('form');
  loginBusy = true; error.textContent = '';
  if (form) busyForm(form, true);
  try {
    const next = await c.request(endpoint, method, body);
    if (gen !== epoch) return;
    state = next;
    if (form) form.reset();
    await enterState();
  } catch (err) { if (gen === epoch) showError(err, error); }
  finally {
    if (gen === epoch) {
      loginBusy = false;
      const current = root.querySelector('form');
      if (current) busyForm(current, false);
      updateLoginTimers();
    }
  }
}

function updateLoginTimers() {
  const resend = document.querySelector('#resend-code');
  if (resend && state?.step === 'code') {
    const status = resendStatus(state);
    resend.textContent = status.label; resend.disabled = loginBusy || status.disabled;
  }
  const expiry = document.querySelector('#qr-expiry');
  if (expiry && state?.step === 'qr') {
    const remaining = Math.max(0, Math.ceil(Number(state.qr?.expires || 0) - Date.now() / 1000));
    expiry.textContent = !state.qr ? '正在生成二维码…' : remaining ? `${remaining} 秒后自动更新` : '二维码已过期，正在等待更新…';
  }
}

function clearQRImage() {
  if (qrImageURL) URL.revokeObjectURL(qrImageURL);
  qrImageURL = ''; qrImageKey = ''; qrPendingKey = '';
}

function renderQRLogin(error) {
  const current = state.qr?.url;
  const picture = el('div', { class: 'qr-picture', id: 'login-qr' });
  if (current && qrImageKey === current && qrImageURL) picture.append(el('img', { src: qrImageURL, alt: '使用手机 Telegram 扫描此二维码', width: 260, height: 260 }));
  else picture.append(text('p', '正在生成二维码…', 'muted'));
  const form = el('form', { class: 'form-stack', onsubmit: event => event.preventDefault() },
    picture, text('p', '', 'hint qr-expiry'),
    text('p', '在已登录的手机 Telegram 中打开：设置 → 设备 → 连接桌面设备，扫描上面的二维码并确认。', 'muted'),
    text('p', '二维码只用于授权你的服务器登录。请勿分享给其他人。', 'hint'), error,
    state.error ? text('p', state.error, 'error') : null,
    button('重新生成二维码', () => void loginAction('/login/qr', error)),
    button('改用手机号登录', () => void loginAction('/login', error, 'DELETE'), 'quiet'),
    button('断开服务器', disconnect, 'quiet'));
  form.querySelector('.qr-expiry').id = 'qr-expiry';
  loginFrame('手机 Telegram 扫码登录', '不需要手机号验证码。手机上需要已有登录的 Telegram 账号。', form);
  busyForm(form, loginBusy); updateLoginTimers();
  if (!current || qrImageKey === current || qrPendingKey === current) return;
  const c = client, gen = epoch;
  qrPendingKey = current;
  void c.fetch('/login/qr.png').then(response => response.blob()).then(blob => {
    if (gen !== epoch || state?.step !== 'qr' || state.qr?.url !== current) return;
    clearQRImage(); qrImageURL = URL.createObjectURL(blob); qrImageKey = current;
    const currentPicture = document.querySelector('#login-qr');
    if (currentPicture) currentPicture.replaceChildren(el('img', { src: qrImageURL, alt: '使用手机 Telegram 扫描此二维码', width: 260, height: 260 }));
  }).catch(err => { if (gen === epoch && state?.qr?.url === current) showError(err, error); })
    .finally(() => { if (qrPendingKey === current) qrPendingKey = ''; });
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
function fileOptions(kind) {
  const raw = media[kind], gif = kind === 'gif';
  const bind = (field, key) => {
    const control = field.querySelector('input,select');
    control.addEventListener(control.tagName === 'SELECT' ? 'change' : 'input', e => { raw[key] = e.target.value; });
    return field;
  };
  const decimal = (value, placeholder) => ({ value, placeholder, inputMode: 'decimal', autocomplete: 'off' });
  const group = el('div', { class: 'upload-options' });
  if (!gif) group.append(bind(input('emoji', '这批贴纸的表情', { value: raw.emoji, placeholder: '🙂', maxLength: 32 }), 'emoji'),
    bind(select('fit', '截取后仍超过 3 秒', [['speed', '加速到 3 秒，保留所选时段'], ['trim', '只保留前 3 秒']], raw.fit), 'fit'));
  const width = bind(input('width', '宽度（像素）', decimal(raw.width, gif ? '2–2048 偶数' : '≤512 偶数')), 'width');
  const height = bind(input('height', '高度（像素）', decimal(raw.height, gif ? '2–2048 偶数' : '≤512 偶数')), 'height');
  width.hidden = height.hidden = raw.size !== 'custom';
  const size = bind(select('size', '输出尺寸', gif ? GIF_SIZES : STICKER_SIZES, raw.size), 'size');
  size.querySelector('select').addEventListener('change', e => { width.hidden = height.hidden = e.target.value !== 'custom'; });
  const details = el('details', { class: 'media-settings', open: mediaOpen[kind], ontoggle: () => { mediaOpen[kind] = details.open; } },
    el('summary', { text: '云端转换设置：截取时段、尺寸、帧率、画质' }),
    text('p', gif
      ? 'GIF 收藏不受 3 秒限制：结束秒留空就保留到文件结尾，整段上传。'
      : 'Telegram 视频贴纸最长 3 秒，无法突破：服务器先截取你填的时段，若仍超过 3 秒，再按“截取后仍超过 3 秒”的选择加速或截断。', 'hint'),
    el('div', { class: 'media-grid' },
      bind(input('start', '开始秒', decimal(raw.start, '0')), 'start'),
      bind(input('end', '结束秒', decimal(raw.end, '留空 = 到结尾')), 'end'),
      size, width, height,
      bind(select('resize', '缩放方式', RESIZE_MODES, raw.resize), 'resize'),
      bind(input('fps', '帧率上限', decimal(raw.fps, `留空 = 自动，最高 ${gif ? 60 : 30}`)), 'fps'),
      bind(select('quality', '画质', QUALITIES, raw.quality), 'quality')),
    text('p', gif
      ? '自动尺寸最长边 1280，小图不放大。补边用黑色填满画布，裁剪从中心截取。时段不能超出原文件时长。'
      : '贴纸较长的一边必须是 512。补边用透明填满画布，裁剪从中心截取；选等比缩放而原图比例无法达到 512 长边时，服务器会拒绝，请改用补边或裁剪。时段不能超出原文件时长。', 'hint'),
    text('p', '选择文件时会保存这份设置，服务器在上传时完成转换。之后修改设置只影响新加入的文件。', 'hint'));
  return [group.childElementCount ? group : null, details];
}
// 选中文件时校验并快照转换设置；不合法就展开设置并说明原因。
function takeMedia(kind, error, details) {
  try { const snap = snapshotMedia(kind, media[kind]); error.textContent = ''; return snap; }
  catch (err) { error.textContent = err.message; if (details) details.open = true; notify(err.message); return null; }
}
function uploadZone(target) {
  const gif = target === '', kind = gif ? 'gif' : 'sticker';
  const picker = el('input', { type: 'file', multiple: true, class: 'file-input', accept: gif ? '.gif,.webp,.webm,.mp4,.mov' : '.gif,.png,.jpg,.jpeg,.webp,.webm,.mp4,.mov', 'aria-label': gif ? '选择动图' : '选择贴纸文件' });
  const error = text('p', '', 'error'); error.role = 'alert';
  const zone = el('section', { class: 'upload-zone', 'aria-label': '文件上传' },
    el('div', { class: 'upload-heading' }, el('div', {}, text('h2', gif ? '添加到 GIF 收藏' : '添加贴纸'),
      text('p', gif ? '拖入动图，上传时由服务器转换并存进 Telegram 的 GIF 收藏。默认保留整段，不受贴纸的 3 秒限制。' : '拖入图片或 GIF，上传时由服务器转换成合适的贴纸格式。', 'muted'),
      text('p', '一次选一个文件会打开编辑器，可以拖动选片段、框选画面并预览成品；一次选多个文件则按下面的设置直接排队。', 'hint')),
      button('选择文件', () => picker.click(), 'primary', !state.ffmpeg)),
    fileOptions(kind), error, picker,
    text('p', '单个文件最大 50 MB。上传与转换过程中请保持本页打开。', 'hint'));
  const take = files => {
    if (!files.length) return;
    if (files.length === 1 && state.ffmpeg) {
      const [file] = validateFiles(files);
      if (file) openEditor({ file, kind, raw: media[kind], openModal, notify, onAdd: snap => { enqueue([file], target, snap); renderPage(); } });
      return;
    }
    const snap = takeMedia(kind, error, zone.querySelector('details'));
    if (snap) enqueue(files, target, snap);
  };
  picker.addEventListener('change', () => { take([...picker.files]); picker.value = ''; });
  zone.addEventListener('dragover', e => { e.preventDefault(); zone.classList.add('dragging'); });
  zone.addEventListener('dragleave', e => { if (!zone.contains(e.relatedTarget)) zone.classList.remove('dragging'); });
  zone.addEventListener('drop', e => { e.preventDefault(); zone.classList.remove('dragging'); take([...e.dataTransfer.files]); });
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

// onClose 在对话框关闭（或内容被换掉）时调用一次，编辑器用它停止播放、释放解码器
let modalCleanup = null;
modal.addEventListener('close', () => { modal.classList.remove('wide'); const done = modalCleanup; modalCleanup = null; done?.(); });
function openModal(title, children, { wide = false, onClose = null } = {}) {
  const previous = modalCleanup; modalCleanup = null; previous?.();
  modal.replaceChildren(el('div', { class: 'dialog-header' }, text('h2', title), button('关闭', () => modal.close(), 'quiet')),
    ...children);
  modal.querySelector('h2').id = 'modal-title';
  modal.classList.toggle('wide', wide);
  modalCleanup = onClose;
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
    sets = sets.filter(s => s.short_name !== name);
    if (pack?.short_name === name) { pack = null; loading = false; }
    if (page === 'sets') renderPage(); notify('贴纸包已删除');
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
    // 第一张和其余文件共用提交时的同一份设置快照。
    const snap = takeMedia('sticker', error, form.querySelector('details')); if (!snap) return;
    busyForm(form, true);
    const c = client, gen = epoch, first = valid[0], rest = valid.slice(1);
    const body = appendMedia(new FormData(), snap); body.set('title', data.get('title')); body.set('short_name', data.get('short_name'));
    body.set('file', first);
    fileList.textContent = '正在转换并上传第一张贴纸，请保持本页打开…';
    void run(async () => {
      const result = await c.request('/sets', 'POST', body); if (gen !== epoch) return;
      pack = result; selected = null; page = 'sets'; sets.unshift(result); modal.close(); renderShell(); renderPack();
      enqueue(rest, result.short_name, snap); notify('贴纸包已创建');
    }, error).finally(() => { busyForm(form, false); fileList.textContent = files.map(f => f.name).join('、'); });
  } }, input('title', '贴纸包名字', { required: true, maxLength: 64, placeholder: '我的表情' }), short, check, checkResult,
    fileOptions('sticker'), inputFileLabel(picker), fileList, error, el('button', { type: 'submit', class: 'primary', text: '创建贴纸包' }));
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
function enqueue(files, target, snap) {
  if (!state?.ffmpeg) { notify('服务器未找到 ffmpeg，无法转换文件。'); return; }
  for (const file of validateFiles(files)) queue.push({ file, target, media: snap, status: 'waiting', error: '' });
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
      const body = appendMedia(new FormData(), job.media); body.set('file', job.file);
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
        text('strong', job.file?.name || job.name), text('span', job.target || 'GIF 收藏', 'hint'), text('span', describeMedia(job.media), 'hint job-media'),
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
