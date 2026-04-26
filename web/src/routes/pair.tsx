import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';
import { AppShell } from '@/components/AppShell';
import { Sidebar } from '@/components/Sidebar';
import { PairForm } from '@/components/PairForm';

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
    <AppShell sidebar={<Sidebar />}>
      <PairForm />
    </AppShell>
  ),
});
