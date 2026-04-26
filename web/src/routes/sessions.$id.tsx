import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';
import { AppShell } from '@/components/AppShell';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/sessions/$id',
  beforeLoad: async () => {
    try {
      await queryClient.fetchQuery({ queryKey: ['me'], queryFn: () => apiClient<MeResponse>('/api/me') });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: '/login' });
      throw e;
    }
  },
  component: function SessionRoute() {
    const { id } = Route.useParams();
    return (
      <AppShell>
        <div className="p-6">Session terminal for {id} (Task 16 fills this in)</div>
      </AppShell>
    );
  },
});
