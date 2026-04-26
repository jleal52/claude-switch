import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PairForm } from '@/components/PairForm';

vi.mock('@tanstack/react-router', async () => {
  const actual = await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router');
  return {
    ...actual,
    Link: ({ to, children, ...rest }: { to?: string; children: React.ReactNode } & React.AnchorHTMLAttributes<HTMLAnchorElement>) =>
      <a href={to ?? '#'} {...rest}>{children}</a>,
  };
});

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<PairForm />', () => {
  it('shows paired confirmation on success', async () => {
    document.cookie = 'cs_csrf=tok';
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ name: 'ireland', os: 'linux', arch: 'amd64', version: '0.1' }),
        { status: 200, headers: { 'content-type': 'application/json' } }),
    );

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<PairForm />, { wrapper: withQuery(qc) });
    await userEvent.type(screen.getByPlaceholderText('ABCD-1234'), 'abcd-1234');
    await userEvent.click(screen.getByRole('button', { name: /approve/i }));

    expect(await screen.findByText(/Paired/)).toBeInTheDocument();
    expect(await screen.findByText(/ireland/)).toBeInTheDocument();
  });
});
