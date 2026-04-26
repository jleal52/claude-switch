import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TopBar } from '@/components/TopBar';

// Stub Link so TopBar can render without a full router context.
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>();
  return {
    ...actual,
    Link: ({ to, children, ...props }: { to: string; children: React.ReactNode; [k: string]: unknown }) => (
      <a href={to} {...props}>{children}</a>
    ),
  };
});

function withQuery() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe('<TopBar />', () => {
  it('shows user email and a logout option', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user: { id: 'u1', email: 'a@x', name: 'A', avatar_url: '', keep_transcripts: false },
          providers_configured: ['github'],
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );
    render(<TopBar />, { wrapper: withQuery() });
    expect(await screen.findByText('a@x')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /open user menu/i }));
    expect(screen.getByRole('menuitem', { name: /logout/i })).toBeInTheDocument();
  });
});
