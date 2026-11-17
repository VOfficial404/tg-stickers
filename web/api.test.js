import { describe, expect, test } from 'bun:test';
import { parseJSON } from './static/api.js';

describe('Telegram IDs in browser responses', () => {
  test('preserves int64 IDs used in thumbnails and mutations', () => {
    const set = parseJSON('{"id":9223372036854775807,"cover":9007199254740993,"stickers":[{"id":-9223372036854775808,"size":1024}],"count":1}');
    expect(set.id).toBe('9223372036854775807');
    expect(set.cover).toBe('9007199254740993');
    expect(set.stickers[0].id).toBe('-9223372036854775808');
    expect(`/stickers/${set.stickers[0].id}`).toBe('/stickers/-9223372036854775808');
    expect(set.count).toBe(1);
    expect(set.stickers[0].size).toBe(1024);
  });
  test('leaves titles, escaped strings, decimals, and exponents intact', () => {
    const input = '{"title":"1234567890123456789 \\"id\\":1234567890123456789","duration":2.9876543210987654,"ratio":1.234567890123456e16,"emoji":"😀"}';
    expect(parseJSON(input)).toEqual(JSON.parse(input));
  });
});
