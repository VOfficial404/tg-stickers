import { describe, expect, test } from 'bun:test';
import { appendMedia, defaultMedia, describeMedia, geometry, snapshotMedia, timing } from './static/media.js';

const fields = media => Object.fromEntries(appendMedia(new FormData(), media).entries());

describe('upload conversion settings', () => {
  test('long GIFs keep the whole clip by default and never send a sticker fit', () => {
    const body = fields(snapshotMedia('gif', defaultMedia('gif')));
    expect(body).toEqual({ start: '0', end: '0', width: '0', height: '0', resize: 'contain', fps: '0', quality: 'standard' });
    expect(body.end).not.toBe('3');
    expect('fit' in body).toBe(false);
  });

  test('sticker uploads send every lowercase field with the chosen range and size', () => {
    const raw = { ...defaultMedia('sticker'), emoji: '😀', fit: 'trim', start: '1.5', end: '4', size: 'custom', width: '512', height: '300', resize: 'pad', fps: '24', quality: 'high' };
    expect(fields(snapshotMedia('sticker', raw))).toEqual({ emoji: '😀', fit: 'trim', start: '1.5', end: '4', width: '512', height: '300', resize: 'pad', fps: '24', quality: 'high' });
  });

  test('a queued snapshot ignores later form edits', () => {
    const raw = { ...defaultMedia('gif'), start: '2', end: '10', size: '640x640', resize: 'crop' };
    const queued = snapshotMedia('gif', raw);
    raw.start = '5'; raw.end = ''; raw.size = 'auto'; raw.resize = 'contain'; raw.quality = 'small';
    expect(fields(queued)).toMatchObject({ start: '2', end: '10', width: '640', height: '640', resize: 'crop', quality: 'standard' });
    expect(describeMedia(queued)).toBe('2 秒–10 秒 · 640×640 裁剪');
  });

  test('rejects invalid time ranges with a specific reason', () => {
    expect(() => snapshotMedia('gif', { ...defaultMedia('gif'), start: '5', end: '5' })).toThrow('结束秒（5）必须大于开始秒（5）');
    expect(() => snapshotMedia('gif', { ...defaultMedia('gif'), start: '-1' })).toThrow('开始秒必须是不小于 0 的数字');
    expect(() => snapshotMedia('sticker', { ...defaultMedia('sticker'), end: 'abc' })).toThrow('结束秒');
  });

  test('rejects sticker sizes Telegram cannot accept', () => {
    const sticker = patch => () => snapshotMedia('sticker', { ...defaultMedia('sticker'), size: 'custom', ...patch });
    expect(sticker({ width: '400', height: '400' })).toThrow('较长的一边正好是 512');
    expect(sticker({ width: '600', height: '512' })).toThrow('2–512');
    expect(sticker({ width: '512', height: '301' })).toThrow('偶数');
    expect(sticker({ width: '512', height: '' })).toThrow('同时填写宽度和高度');
    expect(() => snapshotMedia('sticker', { ...defaultMedia('sticker'), fps: '31' })).toThrow('不能超过 30');
  });

  test('GIF custom sizes and frame rates follow GIF limits', () => {
    expect(snapshotMedia('gif', { ...defaultMedia('gif'), size: 'custom', width: '2048', height: '2', fps: '60' })).toMatchObject({ width: 2048, height: 2, fps: 60 });
    expect(() => snapshotMedia('gif', { ...defaultMedia('gif'), size: 'custom', width: '2050', height: '100' })).toThrow('2–2048');
    expect(() => snapshotMedia('gif', { ...defaultMedia('gif'), fps: '61' })).toThrow('不能超过 60');
    expect(() => snapshotMedia('gif', { ...defaultMedia('gif'), fps: '12.5' })).toThrow('整数');
  });
});

describe('visual editor geometry matches the server', () => {
  const media = patch => snapshotMedia(patch.kind ?? 'gif', { ...defaultMedia(patch.kind ?? 'gif'), ...patch });

  test('an off-centre selection is sent and becomes the base of the output', () => {
    const snap = media({ kind: 'sticker', cropX: '260', cropY: '20', cropW: '120', cropH: '60', size: 'custom', width: '512', height: '256', resize: 'pad' });
    expect(fields(snap)).toMatchObject({ crop_x: '260', crop_y: '20', crop_w: '120', crop_h: '60' });
    expect(describeMedia(snap)).toBe('整段 · 选区 120×60 · 512×256 补边');
    expect(geometry('sticker', { w: 400, h: 100 }, snap)).toMatchObject({ out: { w: 512, h: 256 }, src: { x: 260, y: 20, w: 120, h: 60 }, error: '' });
  });

  test('without a selection no crop fields are uploaded', () => {
    expect('crop_w' in fields(media({}))).toBe(false);
  });

  test('automatic GIF size follows the selection, not the whole frame', () => {
    expect(geometry('gif', { w: 4000, h: 3000 }, media({ cropX: '100', cropY: '100', cropW: '600', cropH: '300' })).out).toEqual({ w: 600, h: 300 });
    expect(geometry('gif', { w: 4000, h: 3000 }, media({})).out).toEqual({ w: 1280, h: 960 });
  });

  test('sticker contain keeps an exact 512 long edge and flags a canvas it cannot fill', () => {
    expect(geometry('sticker', { w: 98, h: 49 }, media({ kind: 'sticker' })).out).toEqual({ w: 512, h: 256 });
    expect(geometry('sticker', { w: 100, h: 100 }, media({ kind: 'sticker', size: '512x288' })).error).toContain('长边必须是 512');
  });

  test('centre crop takes the middle of the selection and fills the canvas', () => {
    const g = geometry('gif', { w: 400, h: 100 }, media({ size: 'custom', width: '100', height: '100', resize: 'crop' }));
    expect(g).toMatchObject({ out: { w: 100, h: 100 }, src: { x: 150, y: 0, w: 100, h: 100 }, dst: { x: 0, y: 0, w: 100, h: 100 } });
  });

  test('pad centres the content inside the canvas', () => {
    expect(geometry('gif', { w: 400, h: 100 }, media({ size: '640x640', resize: 'pad' })).dst).toEqual({ x: 0, y: 240, w: 640, h: 160 });
  });

  test('long sticker clips speed up or trim to 3 seconds; GIF keeps its length', () => {
    expect(timing('gif', 10, media({}))).toMatchObject({ clip: 10, output: 10, rate: 1 });
    const sped = timing('sticker', 10, media({ kind: 'sticker', start: '2', end: '8' }));
    expect(sped.output).toBe(2.99);
    expect(sped.rate).toBeCloseTo(6 / 2.99);
    expect(timing('sticker', 10, media({ kind: 'sticker', start: '2', fit: 'trim' }))).toMatchObject({ start: 2, end: 4.99, output: 2.99, rate: 1 });
  });

  test('partial or tiny selections are rejected', () => {
    expect(() => media({ cropW: '1', cropH: '50' })).toThrow('至少为 2');
    expect(() => media({ cropW: '50' })).toThrow('至少为 2');
  });
});
