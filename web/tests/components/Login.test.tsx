import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Login } from '@/components/Login';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<Login />', () => {
  it('renders a GitHub button when github is configured', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user: null,
          providers_configured: ['github', 'google'],
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<Login />, { wrapper: withQuery(qc) });
    expect(await screen.findByRole('link', { name: /github/i })).toHaveAttribute('href', '/auth/github/login');
    expect(screen.queryByRole('link', { name: /google/i })).toBeNull();
  });
});
