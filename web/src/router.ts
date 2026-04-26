import { createRouter } from '@tanstack/react-router';
import { Route as RootRoute } from './routes/__root';
import { Route as IndexRoute } from './routes/index';
import { Route as LoginRoute } from './routes/login';
import { Route as PairRoute } from './routes/pair';
import { Route as SessionRoute } from './routes/sessions.$id';
import { Route as SettingsRoute } from './routes/settings';

const routeTree = RootRoute.addChildren([
  IndexRoute,
  LoginRoute,
  PairRoute,
  SessionRoute,
  SettingsRoute,
]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
