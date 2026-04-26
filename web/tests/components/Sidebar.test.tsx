import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Sidebar } from '@/components/Sidebar';

// Stub TanStack Router's Link as a plain <a> to avoid pulling in the
// real router context for this unit test.
vi.mock('@tanstack/react-router', async () => {
  const actual = await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router');
  return {
    ...actual,
    Link: ({ to, params, children, ...rest }: { to?: string; params?: Record<string, string>; children: React.ReactNode } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
      let href = to ?? '#';
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v);
        }
      }
      return <a href={href} {...rest}>{children}</a>;
    },
  };
});

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<Sidebar />', () => {
  it('renders a wrapper with its session', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith('/api/wrappers')) {
        return new Response(JSON.stringify([
          { id: 'w1', name: 'ireland', os: 'linux', arch: 'amd64', version: '0.1', paired_at: '', last_seen_at: '', revoked: false },
        ]), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.includes('/api/sessions')) {
        return new Response(JSON.stringify([
          { id: 's1', wrapper_id: 'w1', cwd: '/tmp', account: 'default', status: 'running', created_at: '' },
        ]), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      return new Response('mock-miss', { status: 404 });
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<Sidebar />, { wrapper: withQuery(qc) });
    expect(await screen.findByText('ireland')).toBeInTheDocument();
    expect(await screen.findByText(/\/tmp/)).toBeInTheDocument();
  });
});
