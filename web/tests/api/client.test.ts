import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiClient, ApiError } from '@/api/client';

describe('apiClient', () => {
  beforeEach(() => {
    document.cookie = '';
    vi.restoreAllMocks();
  });

  it('GET attaches credentials', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );
    await apiClient<{ ok: boolean }>('/api/test', { method: 'GET' });
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/test',
      expect.objectContaining({ credentials: 'include' }),
    );
  });

  it('POST adds X-CSRF-Token from cs_csrf cookie', async () => {
    document.cookie = 'cs_csrf=tok-abc';
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(null, { status: 204 }),
    );
    await apiClient('/api/x', { method: 'POST', body: { hi: 1 } });
    const init = fetchSpy.mock.calls[0]![1] as RequestInit;
    const headers = new Headers(init.headers);
    expect(headers.get('X-CSRF-Token')).toBe('tok-abc');
    expect(headers.get('Content-Type')).toBe('application/json');
  });

  it('throws ApiError on non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('nope', { status: 401 }),
    );
    await expect(apiClient('/api/me', { method: 'GET' })).rejects.toBeInstanceOf(ApiError);
    try {
      await apiClient('/api/me', { method: 'GET' });
    } catch (e) {
      expect((e as ApiError).status).toBe(401);
    }
  });
});
