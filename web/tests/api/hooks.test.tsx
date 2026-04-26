import { describe, it, expect, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useMe } from '@/api/hooks';

function withQuery(client: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('useMe', () => {
  it('returns user payload from /api/me', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user: { id: 'u1', email: 'a@x', name: 'A', avatar_url: '', keep_transcripts: false },
          providers_configured: ['github'],
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useMe(), { wrapper: withQuery(qc) });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data?.user.id).toBe('u1');
    expect(result.current.data?.providers_configured).toEqual(['github']);
  });
});
