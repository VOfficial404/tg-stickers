// 上传前的可视化编辑：在浏览器里解码文件，拖动时间轴选片段、在画面上框选区域，旁边实时显示成品。
// 这里只决定参数；真正的转换仍由服务器按这些数字用 ffmpeg 完成，几何和时长计算与服务器一致（media.js）。
import { button, el, input, select, text } from './dom.js';
import { GIF_SIZES, QUALITIES, RESIZE_MODES, STICKER_SIZES, geometry, snapshotMedia, timing } from './media.js';

const VIDEO_NAME = /\.(mp4|m4v|webm|mov)$/i;
const IMAGE_TYPES = { gif: 'image/gif', webp: 'image/webp', png: 'image/png', apng: 'image/apng', jpg: 'image/jpeg', jpeg: 'image/jpeg' };
const MAX_RATE = 16; // 浏览器 playbackRate 的上限
const RATIOS = [['free', '自由'], ['canvas', '跟随成品画布'], ['1:1', '1 : 1'], ['4:3', '4 : 3'], ['16:9', '16 : 9'], ['9:16', '9 : 16']];

const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));
const secs = n => `${Number(n.toFixed(2))} 秒`;

// 把渲染排队：同一时间只解码一帧，fn 在帧还有效时同步调用。
function serial() {
  let chain = Promise.resolve();
  return job => (chain = chain.then(job, job));
}

function videoSource(file) {
  const url = URL.createObjectURL(file);
  const video = el('video', { preload: 'auto', playsInline: true, src: url });
  video.muted = true;
  return new Promise((resolve, reject) => {
    video.addEventListener('loadeddata', () => {
      const queue = serial();
      const seek = t => new Promise(done => {
        if (Math.abs(video.currentTime - t) < 0.004) return done();
        video.addEventListener('seeked', () => done(), { once: true });
        video.currentTime = t;
      });
      resolve({
        w: video.videoWidth, h: video.videoHeight, animated: true,
        duration: Number.isFinite(video.duration) ? video.duration : 0,
        frames: 0,
        render: (t, fn) => queue(async () => { await seek(t); fn(video); }),
        play({ start, end, rate, draw }) {
          let raf = 0;
          video.playbackRate = Math.min(rate, MAX_RATE);
          video.currentTime = start;
          const tick = () => {
            if (video.currentTime >= end || video.ended) video.currentTime = start;
            if (video.paused) void video.play().catch(() => {});
            draw(video, video.currentTime);
            raf = requestAnimationFrame(tick);
          };
          void video.play().catch(() => {});
          raf = requestAnimationFrame(tick);
          return () => { cancelAnimationFrame(raf); video.pause(); };
        },
        close() { video.pause(); video.removeAttribute('src'); video.load(); URL.revokeObjectURL(url); },
      });
    }, { once: true });
    video.addEventListener('error', () => { URL.revokeObjectURL(url); reject(new Error('浏览器解不开这个视频。')); }, { once: true });
  });
}

async function decoderSource(file, type) {
  const decoder = new ImageDecoder({ data: await file.arrayBuffer(), type });
  try {
    await decoder.tracks.ready;
    await decoder.completed;
    const count = decoder.tracks.selectedTrack.frameCount;
    // 每帧的开始时间。帧间隔小于 20 毫秒时按 100 毫秒算，和服务器上 ffmpeg 读 GIF 的规则一致。
    const starts = new Float64Array(count + 1);
    let w = 0, h = 0;
    for (let i = 0; i < count; i++) {
      const { image } = await decoder.decode({ frameIndex: i });
      if (!i) [w, h] = [image.displayWidth, image.displayHeight];
      const d = (image.duration ?? 0) / 1e6;
      starts[i + 1] = starts[i] + (d >= 0.02 ? d : 0.1);
      image.close();
    }
    const indexAt = t => {
      let lo = 0, hi = count - 1;
      while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (starts[mid] <= t) lo = mid; else hi = mid - 1; }
      return lo;
    };
    const queue = serial();
    let cached = null;
    const source = {
      w, h, animated: count > 1, frames: count, duration: count > 1 ? starts[count] : 0,
      render: (t, fn) => queue(async () => {
        const i = indexAt(t);
        if (cached?.i !== i) {
          const { image } = await decoder.decode({ frameIndex: i });
          cached?.image.close();
          cached = { i, image };
        }
        fn(cached.image);
      }),
      play({ start, end, rate, draw }) {
        let raf = 0, busy = false;
        const origin = performance.now(), clip = Math.max(0.01, end - start);
        const tick = now => {
          raf = requestAnimationFrame(tick);
          if (busy) return;
          const t = start + ((now - origin) / 1000 * rate) % clip;
          busy = true;
          void source.render(t, img => draw(img, t)).finally(() => { busy = false; });
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
      },
      close() { cached?.image.close(); cached = null; decoder.close(); },
    };
    return source;
  } catch (err) {
    decoder.close();
    throw err;
  }
}

async function bitmapSource(file) {
  const bitmap = await createImageBitmap(file);
  return {
    w: bitmap.width, h: bitmap.height, animated: false, frames: 1, duration: 0,
    render: (t, fn) => { fn(bitmap); return Promise.resolve(); },
    close() { bitmap.close(); },
  };
}

async function openSource(file) {
  const ext = file.name.split('.').pop().toLowerCase();
  if (file.type.startsWith('video/') || VIDEO_NAME.test(file.name)) return videoSource(file);
  const type = file.type || IMAGE_TYPES[ext];
  if (type && 'ImageDecoder' in window && await ImageDecoder.isTypeSupported(type).catch(() => false)) {
    try { return await decoderSource(file, type); } catch { /* 交给下面只读第一帧 */ }
  }
  const source = await bitmapSource(file);
  // 动图格式却只能读到第一帧：时间轴不可用，但仍可输入秒数
  source.limited = /gif|webp|png/.test(type || '');
  return source;
}

// ctx：app.js 提供的 openModal(title, children, { wide, onClose }) 与 notify
export function openEditor({ file, kind, raw, openModal, notify, onAdd }) {
  const sticker = kind === 'sticker';
  const cfg = { ...raw }; // 画布、缩放、帧率、画质、表情等设置；确认时写回上传区表单
  const status = text('p', `正在读取 ${file.name}…`, 'working');
  const body = el('div', { class: 'editor' }, status);
  let source = null, closed = false, stopPlay = null;
  openModal(`编辑 ${file.name}`, [body], {
    wide: true,
    onClose() { closed = true; stopPlay?.(); source?.close(); },
  });
  openSource(file).then(s => {
    if (closed) { s.close(); return; }
    source = s;
    build();
  }, err => {
    if (closed) return;
    status.className = 'error';
    status.textContent = `${err.message || '浏览器读不出这个文件。'}仍可以不编辑直接上传，由服务器按上传区的设置转换。`;
    body.append(el('div', { class: 'actions' }, button('不编辑，直接加入队列', () => add(raw), 'primary')));
  });

  function add(values) {
    try {
      const snap = snapshotMedia(kind, values);
      onAdd(snap);
      document.querySelector('#modal').close();
    } catch (err) { notify(err.message); }
  }

  function build() {
    const src = { w: source.w, h: source.h };
    const timed = source.animated && source.duration > 0;
    const minSide = Math.max(2, Math.round(Math.min(src.w, src.h) * 0.02));
    const st = { start: 0, end: source.duration, t: 0, sel: null, ratio: 'free' };

    /* 当前参数 → 上传值、几何、时长 */
    const values = () => {
      const v = { ...cfg, start: '', end: '', cropX: '', cropY: '', cropW: '', cropH: '' };
      if (timed) {
        if (st.start > 0.005) v.start = String(Number(st.start.toFixed(3)));
        if (st.end < source.duration - 0.005) v.end = String(Number(st.end.toFixed(3)));
      } else if (source.limited) { v.start = startInput.value; v.end = endInput.value; }
      if (st.sel) Object.assign(v, { cropX: String(st.sel.x), cropY: String(st.sel.y), cropW: String(st.sel.w), cropH: String(st.sel.h) });
      return v;
    };
    const current = () => {
      try {
        const media = snapshotMedia(kind, values());
        const geo = geometry(kind, src, media);
        return { media, geo, time: timing(kind, source.duration, media), error: geo.error };
      } catch (err) { return { error: err.message }; }
    };

    /* 源画面：画布 + 选区框 */
    // 小图放大显示，便于框选；大图缩小
    const scale = Math.min(3, 720 / src.w, 440 / src.h);
    const canvas = el('canvas', { width: Math.max(1, Math.round(src.w * scale)), height: Math.max(1, Math.round(src.h * scale)), 'aria-label': '源画面' });
    const stageCtx = canvas.getContext('2d');
    const handles = ['nw', 'ne', 'sw', 'se'].map(h => el('span', { class: `ed-h ed-h-${h}`, 'data-handle': h, 'aria-hidden': 'true' }));
    const selBox = el('div', { class: 'ed-sel', tabIndex: 0, role: 'group', 'aria-label': '选区，方向键移动，按住 Shift 每次 10 像素' }, handles);
    const effective = el('div', { class: 'ed-effective', 'aria-hidden': 'true' }, text('span', '实际取用'));
    const stage = el('div', { class: 'ed-stage' }, canvas, effective, selBox);
    const stageHint = text('p', '在画面上拖动框出要保留的区域；不框选就使用整个画面。', 'hint');
    const ratio = select('ratio', '选区比例', RATIOS, st.ratio);
    const selInputs = ['x', 'y', 'w', 'h'].map(k => el('input', { type: 'number', min: 0, step: 1, 'aria-label': { x: '选区左边', y: '选区上边', w: '选区宽度', h: '选区高度' }[k], 'data-k': k }));
    const selRow = el('div', { class: 'ed-selrow' }, ratio,
      el('div', { class: 'ed-numbers' }, text('span', 'X'), selInputs[0], text('span', 'Y'), selInputs[1], text('span', '宽'), selInputs[2], text('span', '高'), selInputs[3]),
      button('整个画面', () => { st.sel = null; st.ratio = 'free'; ratio.querySelector('select').value = 'free'; refresh(); }, 'quiet'));

    const pct = (v, total) => `${(v / total) * 100}%`;
    const place = (node, r) => Object.assign(node.style, { left: pct(r.x, src.w), top: pct(r.y, src.h), width: pct(r.w, src.w), height: pct(r.h, src.h) });
    const clampRect = r => {
      const w = clamp(Math.round(r.w), minSide, src.w), h = clamp(Math.round(r.h), minSide, src.h);
      return { x: clamp(Math.round(r.x), 0, src.w - w), y: clamp(Math.round(r.y), 0, src.h - h), w, h };
    };
    const ratioValue = () => {
      if (st.ratio === 'free') return 0;
      if (st.ratio !== 'canvas') { const [a, b] = st.ratio.split(':').map(Number); return a / b; }
      const c = current();
      if (c.media?.width) return c.media.width / c.media.height;
      return sticker ? 1 : 0;
    };
    const fitRatio = (r, value) => {
      let { x, y, w, h } = r;
      if (w / h > value) { const nw = h * value; x += (w - nw) / 2; w = nw; } else { const nh = w / value; y += (h - nh) / 2; h = nh; }
      return clampRect({ x, y, w, h });
    };
    const setSel = r => {
      const full = !r || (r.x === 0 && r.y === 0 && r.w === src.w && r.h === src.h);
      st.sel = full ? null : r;
    };
    const toSource = e => {
      const box = canvas.getBoundingClientRect();
      return [clamp((e.clientX - box.left) / box.width * src.w, 0, src.w), clamp((e.clientY - box.top) / box.height * src.h, 0, src.h)];
    };
    const resizeFrom = (ax, ay, px, py) => {
      let w = Math.abs(px - ax), h = Math.abs(py - ay);
      const value = ratioValue();
      const maxW = px >= ax ? src.w - ax : ax, maxH = py >= ay ? src.h - ay : ay;
      if (value) {
        if (w / h > value) h = w / value; else w = h * value;
        const k = Math.min(1, maxW / w || 1, maxH / h || 1);
        w *= k; h *= k;
      }
      w = clamp(w, minSide, maxW); h = clamp(h, minSide, maxH);
      return clampRect({ x: px >= ax ? ax : ax - w, y: py >= ay ? ay : ay - h, w, h });
    };
    let drag = null;
    stage.addEventListener('pointerdown', e => {
      if (e.button !== 0) return;
      const [px, py] = toSource(e);
      const handle = e.target.dataset?.handle;
      const sel = st.sel ?? { x: 0, y: 0, w: src.w, h: src.h };
      if (handle) {
        const ax = handle.includes('w') ? sel.x + sel.w : sel.x, ay = handle.includes('n') ? sel.y + sel.h : sel.y;
        drag = { kind: 'resize', ax, ay };
      } else if (st.sel && e.target.closest('.ed-sel')) {
        drag = { kind: 'move', dx: px - sel.x, dy: py - sel.y };
      } else {
        drag = { kind: 'new', ax: px, ay: py, sx: e.clientX, sy: e.clientY };
      }
      stage.setPointerCapture(e.pointerId);
      e.preventDefault();
    });
    stage.addEventListener('pointermove', e => {
      if (!drag) return;
      const [px, py] = toSource(e);
      if (drag.kind === 'move') setSel(clampRect({ ...st.sel, x: px - drag.dx, y: py - drag.dy }));
      else if (drag.kind === 'resize') setSel(resizeFrom(drag.ax, drag.ay, px, py));
      else if (Math.hypot(e.clientX - drag.sx, e.clientY - drag.sy) > 4) { drag.kind = 'resize'; setSel(resizeFrom(drag.ax, drag.ay, px, py)); }
      else return;
      refresh();
    });
    const endDrag = () => { drag = null; };
    stage.addEventListener('pointerup', endDrag);
    stage.addEventListener('pointercancel', endDrag);
    selBox.addEventListener('keydown', e => {
      const step = e.shiftKey ? 10 : 1, move = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key];
      if (!move || !st.sel) return;
      e.preventDefault();
      setSel(clampRect({ ...st.sel, x: st.sel.x + move[0], y: st.sel.y + move[1] }));
      refresh();
    });
    ratio.querySelector('select').addEventListener('change', e => {
      st.ratio = e.target.value;
      const value = ratioValue();
      if (value) setSel(fitRatio(st.sel ?? { x: 0, y: 0, w: src.w, h: src.h }, value));
      refresh();
    });
    for (const field of selInputs) field.addEventListener('change', () => {
      const r = { ...(st.sel ?? { x: 0, y: 0, w: src.w, h: src.h }) };
      r[field.dataset.k] = Number(field.value) || 0;
      setSel(clampRect(r));
      refresh();
    });

    /* 时间轴：缩略图条 + 起止把手 + 播放头 */
    const strip = el('div', { class: 'ed-strip' });
    const range = el('div', { class: 'ed-range', 'aria-hidden': 'true' });
    const outside = [el('div', { class: 'ed-outside' }), el('div', { class: 'ed-outside' })];
    const playhead = el('div', { class: 'ed-playhead', 'aria-hidden': 'true' });
    const handle = which => el('div', { class: `ed-handle ed-handle-${which}`, tabIndex: 0, role: 'slider', 'aria-label': which === 'start' ? '片段开始' : '片段结束', 'data-which': which });
    const startHandle = handle('start'), endHandle = handle('end');
    const timeline = el('div', { class: 'ed-timeline' }, strip, ...outside, range, playhead, startHandle, endHandle);
    const playButton = button('▶ 播放片段', () => togglePlay(), 'ed-play', true);
    const startInput = el('input', { type: 'number', min: 0, step: 0.05, 'aria-label': '开始秒' });
    const endInput = el('input', { type: 'number', min: 0, step: 0.05, 'aria-label': '结束秒', placeholder: source.limited ? '留空到结尾' : '' });
    const timeRow = el('div', { class: 'ed-timerow' }, playButton,
      el('label', { class: 'ed-num' }, text('span', '开始'), startInput, text('span', '秒')),
      el('label', { class: 'ed-num' }, text('span', '结束'), endInput, text('span', '秒')),
      text('span', '', 'hint ed-clip'));
    const timeOf = e => { const box = timeline.getBoundingClientRect(); return clamp((e.clientX - box.left) / box.width, 0, 1) * source.duration; };
    const gap = Math.min(0.05, source.duration / 2);
    const setStart = t => { st.start = clamp(t, 0, st.end - gap); };
    const setEnd = t => { st.end = clamp(t, st.start + gap, source.duration); };
    let tdrag = null;
    timeline.addEventListener('pointerdown', e => {
      if (e.button !== 0) return;
      stop();
      tdrag = e.target.dataset?.which || 'seek';
      timeline.setPointerCapture(e.pointerId);
      moveTime(e);
      e.preventDefault();
    });
    const moveTime = e => {
      const t = timeOf(e);
      if (tdrag === 'start') { setStart(t); st.t = st.start; }
      else if (tdrag === 'end') { setEnd(t); st.t = st.end; }
      else st.t = t;
      refresh();
    };
    timeline.addEventListener('pointermove', e => { if (tdrag) moveTime(e); });
    timeline.addEventListener('pointerup', () => { tdrag = null; });
    timeline.addEventListener('pointercancel', () => { tdrag = null; });
    for (const h of [startHandle, endHandle]) h.addEventListener('keydown', e => {
      const step = (e.shiftKey ? 1 : 0.1) * ({ ArrowLeft: -1, ArrowDown: -1, ArrowRight: 1, ArrowUp: 1 }[e.key] ?? 0);
      if (!step) return;
      e.preventDefault(); stop();
      if (h === startHandle) { setStart(st.start + step); st.t = st.start; } else { setEnd(st.end + step); st.t = st.end; }
      refresh();
    });
    startInput.addEventListener('change', () => { if (timed) { setStart(Number(startInput.value) || 0); st.t = st.start; } stop(); refresh(); });
    endInput.addEventListener('change', () => { if (timed) { setEnd(Number(endInput.value) || source.duration); st.t = st.end; } stop(); refresh(); });

    /* 成品预览 */
    const outCanvas = el('canvas', { class: sticker ? 'ed-out checker' : 'ed-out', 'aria-label': '成品预览' });
    const outCtx = outCanvas.getContext('2d');
    const outInfo = text('p', '', 'ed-outinfo');
    const errorLine = text('p', '', 'error'); errorLine.role = 'alert';

    /* 设置 */
    const bind = (field, key, after) => {
      const control = field.querySelector('input,select');
      control.addEventListener(control.tagName === 'SELECT' ? 'change' : 'input', e => {
        cfg[key] = e.target.value; after?.(); stop();
        if (st.ratio === 'canvas') { const value = ratioValue(); if (value) setSel(fitRatio({ x: 0, y: 0, w: src.w, h: src.h }, value)); }
        refresh();
      });
      return field;
    };
    const widthField = bind(input('width', '宽', { value: cfg.width, inputMode: 'numeric', placeholder: sticker ? '≤512 偶数' : '偶数' }), 'width');
    const heightField = bind(input('height', '高', { value: cfg.height, inputMode: 'numeric', placeholder: sticker ? '≤512 偶数' : '偶数' }), 'height');
    const showCustom = () => { widthField.hidden = heightField.hidden = cfg.size !== 'custom'; };
    showCustom();
    const settings = el('div', { class: 'ed-settings' },
      ...(sticker ? [bind(input('emoji', '表情', { value: cfg.emoji, maxLength: 32, placeholder: '🙂' }), 'emoji'),
        bind(select('fit', '片段超过 3 秒', [['speed', '加速到 3 秒'], ['trim', '只留前 3 秒']], cfg.fit), 'fit')] : []),
      bind(select('size', '成品画布', sticker ? STICKER_SIZES : GIF_SIZES, cfg.size), 'size', showCustom), widthField, heightField,
      bind(select('resize', '放进画布的方式', RESIZE_MODES, cfg.resize), 'resize'),
      bind(input('fps', '帧率上限', { value: cfg.fps, inputMode: 'numeric', placeholder: `自动，最高 ${sticker ? 30 : 60}` }), 'fps'),
      bind(select('quality', '画质', QUALITIES, cfg.quality), 'quality'));

    const addButton = button('加入上传队列', () => {
      // 画布、缩放方式等设置记到上传区，下个文件沿用；片段和选区只属于这个文件
      for (const key of ['size', 'width', 'height', 'resize', 'fps', 'quality', 'emoji', 'fit']) if (key in cfg) raw[key] = cfg[key];
      add(values());
    }, 'primary');
    const sourceInfo = [`${src.w} × ${src.h}`];
    if (timed) sourceInfo.push(secs(source.duration));
    if (source.frames > 1) sourceInfo.push(`${source.frames} 帧`);
    if (!source.animated && !source.limited) sourceInfo.push('静态图片');

    status.remove();
    body.append(
      el('div', { class: 'ed-main' },
        el('div', { class: 'ed-source' }, el('div', { class: 'ed-caption' }, text('strong', '原画面'), text('span', sourceInfo.join(' · '), 'hint')),
          stage, stageHint, selRow),
        el('div', { class: 'ed-side' }, el('div', { class: 'ed-caption' }, text('strong', '成品预览'), text('span', '服务器按这些参数转换', 'hint')),
          el('div', { class: 'ed-outbox' }, outCanvas), outInfo, errorLine, settings)),
      ...(timed || source.limited ? [el('section', { class: 'ed-time', 'aria-label': '片段' }, timed ? timeline : text('p', '这个浏览器不能逐帧读取动图，只显示第一帧。片段可以直接填秒数；Chrome、Edge 可以拖动选择。', 'hint'), timeRow)] : []),
      el('div', { class: 'actions' }, button('取消', () => document.querySelector('#modal').close()), addButton));
    if (!timed) { playButton.hidden = true; timeline.hidden = true; }

    function drawFrame(img, t) {
      stageCtx.clearRect(0, 0, canvas.width, canvas.height);
      stageCtx.drawImage(img, 0, 0, canvas.width, canvas.height);
      const c = current();
      if (c.geo) {
        const { out, src: from, dst } = c.geo;
        const k = Math.min(1, 300 / out.w, 300 / out.h);
        const w = Math.max(1, Math.round(out.w * k)), h = Math.max(1, Math.round(out.h * k));
        if (outCanvas.width !== w || outCanvas.height !== h) Object.assign(outCanvas, { width: w, height: h });
        outCtx.clearRect(0, 0, w, h);
        if (!sticker && c.media.resize === 'pad') { outCtx.fillStyle = '#000'; outCtx.fillRect(0, 0, w, h); }
        outCtx.drawImage(img, from.x, from.y, from.w, from.h, dst.x * k, dst.y * k, dst.w * k, dst.h * k);
      }
      if (timed && t !== undefined) playhead.style.left = pct(t, source.duration);
    }

    // 拖动时只画最新请求的那一帧，不排长队
    let wanted = null, drawing = false;
    function showFrame(t) {
      wanted = t;
      if (drawing) return;
      drawing = true;
      const next = wanted; wanted = null;
      void source.render(next, img => drawFrame(img, next)).finally(() => {
        drawing = false;
        if (wanted !== null && !closed) showFrame(wanted);
      });
    }

    function refresh() {
      const c = current();
      errorLine.textContent = c.error || '';
      addButton.disabled = Boolean(c.error);
      // 选区和实际取用的区域（居中裁剪时会比选区更小）
      selBox.hidden = !st.sel;
      if (st.sel) place(selBox, st.sel);
      stage.classList.toggle('has-sel', Boolean(st.sel));
      const sel = st.sel ?? { x: 0, y: 0, w: src.w, h: src.h };
      selInputs.forEach((field, i) => { if (document.activeElement !== field) field.value = [sel.x, sel.y, sel.w, sel.h][i]; });
      const from = c.geo?.src;
      effective.hidden = !from || (from.w === sel.w && from.h === sel.h);
      if (from) place(effective, from);
      if (c.geo) {
        const lines = [`${c.geo.out.w} × ${c.geo.out.h}`];
        if (timed || source.limited) {
          const tm = c.time;
          if (timed) lines.push(`${secs(tm.output)}`);
          if (sticker && tm.clip > 3) lines.push(c.media.fit === 'trim' ? '超过 3 秒的部分会截掉' : `加速 ${tm.rate.toFixed(2)} 倍`);
        }
        if (c.media.fps) lines.push(`≤ ${c.media.fps} fps`);
        outInfo.textContent = lines.join(' · ');
      }
      if (timed) {
        const tm = c.time ?? { end: st.end };
        place2(range, st.start, st.end);
        place2(outside[0], 0, st.start);
        place2(outside[1], st.end, source.duration);
        startHandle.style.left = pct(st.start, source.duration);
        endHandle.style.left = pct(st.end, source.duration);
        for (const [h, v] of [[startHandle, st.start], [endHandle, st.end]]) {
          h.setAttribute('aria-valuemin', '0'); h.setAttribute('aria-valuemax', String(source.duration));
          h.setAttribute('aria-valuenow', v.toFixed(2)); h.setAttribute('aria-valuetext', secs(v));
        }
        if (document.activeElement !== startInput) startInput.value = Number(st.start.toFixed(2));
        if (document.activeElement !== endInput) endInput.value = Number(st.end.toFixed(2));
        timeRow.querySelector('.ed-clip').textContent = `片段 ${secs(st.end - st.start)}${sticker && tm.end < st.end ? `，成品只到 ${secs(tm.end)}` : ''}`;
      }
      if (!stopPlay) showFrame(timed ? clamp(st.t, 0, source.duration) : 0);
    }
    function place2(node, from, to) { Object.assign(node.style, { left: pct(from, source.duration), width: pct(Math.max(0, to - from), source.duration) }); }

    function stop() {
      if (!stopPlay) return;
      stopPlay(); stopPlay = null;
      playButton.textContent = '▶ 播放片段';
    }
    function togglePlay() {
      if (stopPlay) { stop(); refresh(); return; }
      const c = current();
      if (!c.time) return;
      // 贴纸会按成品的速度播放；超过浏览器上限时按上限播，转换结果仍按实际倍数
      stopPlay = source.play({ start: c.time.start, end: Math.min(c.time.end, source.duration), rate: c.time.rate, draw: (img, t) => { st.t = t; drawFrame(img, t); } });
      playButton.textContent = '❚❚ 暂停';
    }

    async function filmstrip() {
      const n = clamp(Math.round((timeline.clientWidth || 600) / 72), 6, 14);
      const h = 52, w = Math.max(24, Math.round(h * src.w / src.h));
      for (let i = 0; i < n && !closed; i++) {
        const thumb = el('canvas', { width: w, height: h });
        strip.append(thumb);
        const ctx = thumb.getContext('2d');
        await source.render(source.duration * (i + 0.5) / n, img => ctx.drawImage(img, 0, 0, w, h));
      }
    }

    refresh();
    if (timed) {
      // 视频解码和播放共用一个元素，缩略图生成完再允许播放
      void filmstrip().finally(() => { if (!closed) { playButton.disabled = false; refresh(); } });
    }
  }
}
