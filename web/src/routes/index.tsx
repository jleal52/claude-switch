import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';
import { AppShell } from '@/components/AppShell';
import { Sidebar } from '@/components/Sidebar';

async function ensureAuthed() {
  try {
    await queryClient.fetchQuery({
      queryKey: ['me'],
      queryFn: () => apiClient<MeResponse>('/api/me'),
    });
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      throw redirect({ to: '/login' });
    }
    throw e;
  }
}

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/',
  beforeLoad: ensureAuthed,
  component: () => (
    <AppShell sidebar={<Sidebar />}>
      <div className="grid h-full place-items-center text-muted-foreground">
        Select a session or pair a wrapper to start.
      </div>
    </AppShell>
  ),
});
