import { TopBar } from './TopBar';
import { ReactNode } from 'react';

export function AppShell({
  sidebar,
  children,
}: {
  sidebar?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex h-screen flex-col">
      <TopBar />
      <div className="flex flex-1 overflow-hidden">
        {sidebar && (
          <aside className="hidden w-72 shrink-0 border-r bg-muted/40 md:block">
            {sidebar}
          </aside>
        )}
        <main className="flex-1 overflow-hidden">{children}</main>
      </div>
    </div>
  );
}
