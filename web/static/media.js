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
  const base = { start: '', end: '', size: 'auto', width: '', height: '', resize: 'contain', fps: '', quality: 'standard' };
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
  const media = { kind, start, end, width, height, resize, fps, quality };
  if (kind === 'sticker') {
    if (raw.fit !== 'speed' && raw.fit !== 'trim') throw new Error(`未知的超时处理方式「${raw.fit}」。`);
    media.emoji = String(raw.emoji ?? ''); media.fit = raw.fit;
  }
  return Object.freeze(media);
}

// 写入上传请求的字段。GIF 不带 fit，服务器不会把 GIF 收藏截到 3 秒。
export function appendMedia(body, media) {
  if (media.kind === 'sticker') { body.set('emoji', media.emoji); body.set('fit', media.fit); }
  for (const key of ['start', 'end', 'width', 'height', 'resize', 'fps', 'quality']) body.set(key, String(media[key]));
  return body;
}

const seconds = n => `${Number(n.toFixed(3))} 秒`;
const RESIZE_SHORT = { contain: '等比', pad: '补边', crop: '裁剪' };
// 队列里展示的简短设置：时间段 + 尺寸，以及非默认的帧率/画质。
export function describeMedia(media) {
  const time = !media.start && !media.end ? '整段' : media.end ? `${seconds(media.start)}–${seconds(media.end)}` : `${seconds(media.start)}起到结尾`;
  const size = media.width ? `${media.width}×${media.height} ${RESIZE_SHORT[media.resize]}` : '自动尺寸';
  const parts = [time, size];
  if (media.kind === 'sticker' && media.fit === 'trim') parts.push('超 3 秒截断');
  if (media.fps) parts.push(`≤${media.fps} fps`);
  if (media.quality !== 'standard') parts.push(QUALITIES.find(([v]) => v === media.quality)[1]);
  return parts.join(' · ');
}
