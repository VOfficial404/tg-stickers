import { describe, expect, test } from 'bun:test';
import { appendMedia, defaultMedia, describeMedia, snapshotMedia } from './static/media.js';

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
