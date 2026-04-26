import { createRoute } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/login',
  component: () => (
    <main className="grid min-h-screen place-items-center">
      <div className="space-y-3 rounded-lg border bg-card p-8 text-center shadow">
        <h1 className="text-xl font-semibold">Sign in to claude-switch</h1>
        <p className="text-sm text-muted-foreground">Login providers will appear in Task 7.</p>
      </div>
    </main>
  ),
});
