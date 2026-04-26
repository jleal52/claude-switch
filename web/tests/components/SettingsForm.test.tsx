import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SettingsForm } from '@/components/SettingsForm';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<SettingsForm />', () => {
  it('clamps retention to 1-90 and POSTs settings', async () => {
    document.cookie = 'cs_csrf=tok';
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (url.endsWith('/api/me') && (init?.method ?? 'GET') === 'GET') {
        return new Response(JSON.stringify({
          user: { id: 'u1', email: 'a@x', keep_transcripts: false },
          providers_configured: ['github'],
        }), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.endsWith('/api/me/settings') && init?.method === 'POST') {
        return new Response(null, { status: 204 });
      }
      return new Response('miss', { status: 404 });
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<SettingsForm />, { wrapper: withQuery(qc) });

    await userEvent.click(await screen.findByRole('checkbox'));
    await userEvent.clear(screen.getByRole('spinbutton'));
    await userEvent.type(screen.getByRole('spinbutton'), '500');
    await userEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find(([u, i]) =>
        String(u).endsWith('/api/me/settings') && (i as RequestInit | undefined)?.method === 'POST',
      );
      expect(call).toBeTruthy();
      const body = JSON.parse(String((call![1] as RequestInit).body));
      expect(body.transcript_retention_days).toBe(90);
    });
  });
});
