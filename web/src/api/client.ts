// Lightweight typed fetch wrapper. Always sends cookies. On mutating
// methods (POST/PUT/PATCH/DELETE), reads the cs_csrf cookie and mirrors
// it into the X-CSRF-Token header per the server's double-submit pattern.
//
// Body objects are JSON-encoded automatically; pass a string/FormData/Blob
// to skip that.

export class ApiError extends Error {
  constructor(public status: number, public body: string) {
    super(`api ${status}: ${body}`);
  }
}

export interface ApiOptions extends Omit<RequestInit, 'body'> {
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
}

const MUTATING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

export async function apiClient<T>(path: string, opts: ApiOptions = {}): Promise<T> {
  const method = (opts.method ?? 'GET').toUpperCase();
  const url = withQuery(path, opts.query);

  const headers = new Headers(opts.headers);
  let body: BodyInit | undefined;
  if (opts.body !== undefined) {
    if (typeof opts.body === 'string' || opts.body instanceof FormData || opts.body instanceof Blob) {
      body = opts.body as BodyInit;
    } else {
      body = JSON.stringify(opts.body);
      if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    }
  }
  if (MUTATING.has(method)) {
    const csrf = readCookie('cs_csrf');
    if (csrf) headers.set('X-CSRF-Token', csrf);
  }

  const resp = await fetch(url, {
    ...opts,
    method,
    headers,
    body,
    credentials: 'include',
  });

  if (!resp.ok) {
    const text = await safeText(resp);
    throw new ApiError(resp.status, text);
  }
  if (resp.status === 204) return undefined as T;
  const ct = resp.headers.get('content-type') ?? '';
  if (ct.includes('application/json')) return (await resp.json()) as T;
  return (await resp.text()) as unknown as T;
}

function readCookie(name: string): string | undefined {
  const target = `${name}=`;
  for (const c of document.cookie.split(';')) {
    const t = c.trim();
    if (t.startsWith(target)) return decodeURIComponent(t.slice(target.length));
  }
  return undefined;
}

function withQuery(path: string, q?: ApiOptions['query']): string {
  if (!q) return path;
  const usp = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined) continue;
    usp.set(k, String(v));
  }
  const qs = usp.toString();
  return qs ? `${path}?${qs}` : path;
}

async function safeText(r: Response): Promise<string> {
  try { return await r.text(); } catch { return ''; }
}
