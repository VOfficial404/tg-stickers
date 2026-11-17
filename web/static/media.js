// 上传时交给服务器的转换参数。表单保存原始字符串，选中文件时用 snapshotMedia 校验并冻结一份副本，
// 之后表单再怎么改，已经排队（包括失败重试）的文件仍按当时的参数上传。

export const STICKER_SIZES = [['auto', '自动（长边 512）'], ['512x512', '512 × 512 方形'], ['512x288', '512 × 288 横向 16:9'], ['288x512', '288 × 512 竖向 9:16'], ['512x384', '512 × 384 横向 4:3'], ['custom', '自定义']];
export const GIF_SIZES = [['auto', '自动（最长 1280，不放大）'], ['480x480', '480 × 480'], ['640x640', '640 × 640'], ['1280x720', '1280 × 720 横向'], ['720x1280', '720 × 1280 竖向'], ['custom', '自定义']];
export const RESIZE_MODES = [['contain', '等比缩放（不裁剪）'], ['pad', '补边到画布'], ['crop', '居中裁剪到画布']];
export const QUALITIES = [['standard', '标准'], ['high', '高画质'], ['small', '小体积']];

const LIMITS = {
  sticker: { min: 2, max: 512, fps: 30 },
  gif: { min: 2, max: 2048, fps: 60 },
};

export function defaultMedia(kind) {
  const base = { start: '', end: '', size: 'auto', width: '', height: '', resize: 'contain', fps: '', quality: 'standard', cropX: '', cropY: '', cropW: '', cropH: '' };
  return kind === 'sticker' ? { emoji: '🙂', fit: 'speed', ...base } : base;
}

function number(raw, label, integer) {
  const value = String(raw ?? '').trim();
  if (!value) return 0;
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) throw new Error(`${label}必须是不小于 0 的数字，现在是「${value}」。`);
  if (integer && !Number.isInteger(n)) throw new Error(`${label}必须是整数，现在是「${value}」。`);
  return n;
}

function dimensions(kind, raw) {
  if (raw.size === 'auto') return [0, 0];
  if (raw.size !== 'custom') {
    const preset = (kind === 'sticker' ? STICKER_SIZES : GIF_SIZES).find(([value]) => value === raw.size);
    if (!preset) throw new Error(`未知的尺寸预设「${raw.size}」。`);
    return raw.size.split('x').map(Number);
  }
  const width = number(raw.width, '宽度', true), height = number(raw.height, '高度', true);
  if (!width || !height) throw new Error('自定义尺寸需要同时填写宽度和高度；不想指定请选“自动”。');
  const { min, max } = LIMITS[kind];
  if (width % 2 || height % 2) throw new Error(`宽高必须是偶数，现在是 ${width} × ${height}。`);
  if (width < min || height < min || width > max || height > max) throw new Error(`${kind === 'sticker' ? '贴纸' : 'GIF'}宽高必须在 ${min}–${max} 之间，现在是 ${width} × ${height}。`);
  if (kind === 'sticker' && Math.max(width, height) !== 512) throw new Error(`Telegram 要求贴纸较长的一边正好是 512，现在是 ${width} × ${height}。`);
  return [width, height];
}

// 校验表单并返回冻结的参数副本；不合法时抛出带具体原因的 Error。
export function snapshotMedia(kind, raw) {
  const start = number(raw.start, '开始秒', false), end = number(raw.end, '结束秒', false);
  if (end && end <= start) throw new Error(`结束秒（${end}）必须大于开始秒（${start}）；留空表示一直到文件结尾。`);
  const [width, height] = dimensions(kind, raw);
  const resize = raw.resize || 'contain';
  if (!RESIZE_MODES.some(([value]) => value === resize)) throw new Error(`未知的缩放方式「${resize}」。`);
  const fps = number(raw.fps, '帧率上限', true);
  if (fps > LIMITS[kind].fps) throw new Error(`${kind === 'sticker' ? '贴纸' : 'GIF'}帧率上限不能超过 ${LIMITS[kind].fps}，现在是 ${fps}。`);
  const quality = raw.quality || 'standard';
  if (!QUALITIES.some(([value]) => value === quality)) throw new Error(`未知的画质选项「${quality}」。`);
  const media = { kind, start, end, width, height, resize, fps, quality, ...selection(raw) };
  if (kind === 'sticker') {
    if (raw.fit !== 'speed' && raw.fit !== 'trim') throw new Error(`未知的超时处理方式「${raw.fit}」。`);
    media.emoji = String(raw.emoji ?? ''); media.fit = raw.fit;
  }
  return Object.freeze(media);
}

// 源画面选区：四个整数，宽高为 0 表示整幅画面。是否超出原画面由编辑器和服务器检查。
function selection(raw) {
  const cropX = number(raw.cropX, '选区左边', true), cropY = number(raw.cropY, '选区上边', true);
  const cropW = number(raw.cropW, '选区宽度', true), cropH = number(raw.cropH, '选区高度', true);
  if (!cropW && !cropH) return { cropX: 0, cropY: 0, cropW: 0, cropH: 0 };
  if (cropW < 2 || cropH < 2) throw new Error(`选区宽高至少为 2 像素，现在是 ${cropW} × ${cropH}。`);
  return { cropX, cropY, cropW, cropH };
}

// 写入上传请求的字段。GIF 不带 fit，服务器不会把 GIF 收藏截到 3 秒。
export function appendMedia(body, media) {
  if (media.kind === 'sticker') { body.set('emoji', media.emoji); body.set('fit', media.fit); }
  for (const key of ['start', 'end', 'width', 'height', 'resize', 'fps', 'quality']) body.set(key, String(media[key]));
  if (media.cropW) for (const [key, field] of [['cropX', 'crop_x'], ['cropY', 'crop_y'], ['cropW', 'crop_w'], ['cropH', 'crop_h']]) body.set(field, String(media[key]));
  return body;
}

const seconds = n => `${Number(n.toFixed(3))} 秒`;
const RESIZE_SHORT = { contain: '等比', pad: '补边', crop: '裁剪' };
// 队列里展示的简短设置：时间段 + 尺寸，以及非默认的帧率/画质。
export function describeMedia(media) {
  const time = !media.start && !media.end ? '整段' : media.end ? `${seconds(media.start)}–${seconds(media.end)}` : `${seconds(media.start)}起到结尾`;
  const size = media.width ? `${media.width}×${media.height} ${RESIZE_SHORT[media.resize]}` : '自动尺寸';
  const parts = [time, ...(media.cropW ? [`选区 ${media.cropW}×${media.cropH}`] : []), size];
  if (media.kind === 'sticker' && media.fit === 'trim') parts.push('超 3 秒截断');
  if (media.fps) parts.push(`≤${media.fps} fps`);
  if (media.quality !== 'standard') parts.push(QUALITIES.find(([v]) => v === media.quality)[1]);
  return parts.join(' · ');
}

function evenFit(w, h, side) {
  if (w > side || h > side) [w, h] = w >= h ? [side, Math.floor(h * side / w)] : [Math.floor(w * side / h), side];
  return [Math.max(2, w & ~1), Math.max(2, h & ~1)];
}

// 与服务端 convert.planMedia 相同的几何：先取选区，再按缩放方式放进画布。
// 返回成品尺寸 out、源画面里实际取用的区域 src（原始像素），以及它在成品里的位置 dst。
export function geometry(kind, source, media) {
  const base = media.cropW ? { x: media.cropX, y: media.cropY, w: media.cropW, h: media.cropH } : { x: 0, y: 0, w: source.w, h: source.h };
  let [w, h] = [media.width, media.height];
  if (!w) [w, h] = kind === 'sticker' ? [512, 512] : evenFit(base.w, base.h, 1280);
  const mode = media.resize || 'contain';
  let sw, sh;
  if (base.w * h >= base.h * w) [sw, sh] = [w, Math.max(2, Math.floor(base.h * w / base.w) & ~1)];
  else [sw, sh] = [Math.max(2, Math.floor(base.w * h / base.h) & ~1), h];
  let src = base;
  if (mode === 'crop') {
    let [cw, ch] = [base.w, base.h];
    if (cw * h > ch * w) cw = Math.max(1, Math.floor(ch * w / h)); else ch = Math.max(1, Math.floor(cw * h / w));
    src = { x: base.x + Math.floor((base.w - cw) / 2), y: base.y + Math.floor((base.h - ch) / 2), w: cw, h: ch };
    [sw, sh] = [w, h];
  }
  const out = mode === 'pad' ? { w, h } : { w: sw, h: sh };
  const dst = { x: Math.floor((out.w - sw) / 2), y: Math.floor((out.h - sh) / 2), w: sw, h: sh };
  const error = kind === 'sticker' && Math.max(out.w, out.h) !== 512
    ? `等比缩放后成品是 ${out.w} × ${out.h}，贴纸长边必须是 512。请改用补边或居中裁剪，或换一个画布尺寸。` : '';
  return { out, src, dst, error };
}

export const STICKER_SECONDS = 2.99;
// 片段与成品时长：贴纸超过 3 秒时加速或只保留开头，GIF 原样保留。
export function timing(kind, duration, media) {
  const start = media.start || 0, end = media.end || duration;
  const clip = Math.max(0, end - start);
  if (kind !== 'sticker' || clip <= 3) return { start, end, clip, output: clip, rate: 1 };
  if (media.fit === 'trim') return { start, end: start + STICKER_SECONDS, clip, output: STICKER_SECONDS, rate: 1 };
  return { start, end, clip, output: STICKER_SECONDS, rate: clip / STICKER_SECONDS };
}
