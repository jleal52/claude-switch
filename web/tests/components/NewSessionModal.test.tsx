import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { NewSessionModal } from '@/components/NewSessionModal';

vi.mock('@tanstack/react-router', async () => {
  const actual = await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router');
  return {
    ...actual,
    useNavigate: () => () => {},
    Link: ({ to, children, ...rest }: { to?: string; children: React.ReactNode } & React.AnchorHTMLAttributes<HTMLAnchorElement>) =>
      <a href={to ?? '#'} {...rest}>{children}</a>,
  };
});

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<NewSessionModal />', () => {
  it('POSTs /api/sessions with form values', async () => {
    document.cookie = 'cs_csrf=tok';
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (url.endsWith('/api/wrappers')) {
        return new Response(JSON.stringify([
          { id: 'w1', name: 'ireland', os: 'linux', arch: 'amd64', version: '0.1', paired_at: '', last_seen_at: '', revoked: false },
        ]), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.endsWith('/api/sessions') && (init?.method ?? 'GET') === 'POST') {
        const body = JSON.parse(String(init?.body ?? '{}'));
        return new Response(JSON.stringify({
          id: 'new-id', wrapper_id: body.wrapper_id, cwd: body.cwd, account: 'default',
          status: 'starting', created_at: '',
        }), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      return new Response('mock-miss', { status: 404 });
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <NewSessionModal defaultWrapperID="w1" trigger={<Button>Open</Button>} />,
      { wrapper: withQuery(qc) },
    );

    await userEvent.click(screen.getByRole('button', { name: 'Open' }));
    await userEvent.type(await screen.findByPlaceholderText('/home/user'), '/tmp');
    await userEvent.click(screen.getByRole('button', { name: /create/i }));

    await waitFor(() => {
      const post = fetchSpy.mock.calls.find(([u, i]) => String(u).endsWith('/api/sessions') && (i as RequestInit | undefined)?.method === 'POST');
      expect(post).toBeTruthy();
    });
  });
});
