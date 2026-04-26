import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';
import { AppShell } from '@/components/AppShell';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/pair',
  beforeLoad: async () => {
    try {
      await queryClient.fetchQuery({ queryKey: ['me'], queryFn: () => apiClient<MeResponse>('/api/me') });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: '/login' });
      throw e;
    }
  },
  component: () => (
    <AppShell>
      <div className="p-6">Pair form (Task 11 fills this in)</div>
    </AppShell>
  ),
});
