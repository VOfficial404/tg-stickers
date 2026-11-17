// Telegram 文档 ID 是 int64。先跳过 JSON 字符串，再把长整数保留为字符串，避免 JSON.parse 四舍五入。
export function parseJSON(text) {
  return JSON.parse(text.replace(/"(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g,
    token => /^-?\d{16,}$/.test(token) ? `"${token}"` : token));
}

export class APIError extends Error {
  constructor(message, status) { super(message); this.status = status; }
}

export class Client {
  constructor(token) {
    this.token = token;
    this.abort = new AbortController();
    this.urls = new Map();
  }
  close() {
    this.abort.abort();
    for (const url of this.urls.values()) URL.revokeObjectURL(url);
    this.urls.clear();
  }
  async fetch(path, options = {}) {
    const headers = new Headers(options.headers);
    headers.set('Authorization', `Bearer ${this.token}`);
    const response = await fetch(`/api${path}`, { ...options, headers, signal: this.abort.signal, cache: 'no-store' });
    if (!response.ok) {
      const text = await response.text();
      let message;
      try { message = parseJSON(text).error; } catch { /* 反向代理也可能返回 HTML */ }
      throw new APIError(message || `服务器返回 HTTP ${response.status}`, response.status);
    }
    return response;
  }
  async request(path, method = 'GET', body) {
    const options = { method };
    if (body instanceof FormData) options.body = body;
    else if (body !== undefined) {
      options.headers = { 'Content-Type': 'application/json' };
      options.body = JSON.stringify(body);
    }
    return parseJSON(await (await this.fetch(path, options)).text());
  }
  async thumb(type, id) {
    const key = `${type}/${id}`;
    if (this.urls.has(key)) return this.urls.get(key);
    const blob = await (await this.fetch(`/${key}/thumb`)).blob();
    if (this.abort.signal.aborted) throw new DOMException('已断开连接', 'AbortError');
    const url = URL.createObjectURL(blob);
    this.urls.set(key, url);
    return url;
  }
}
