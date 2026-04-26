import { TopBar } from './TopBar';
import { ReactNode, useState } from 'react';
import { Sheet, SheetContent } from '@/components/ui/sheet';

export function AppShell({
  sidebar,
  children,
}: {
  sidebar?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex h-screen flex-col">
      <TopBar onOpenSidebar={() => setOpen(true)} hasSidebar={!!sidebar} />
      <div className="flex flex-1 overflow-hidden">
        {sidebar && (
          <aside className="hidden w-72 shrink-0 border-r bg-muted/40 md:block">
            {sidebar}
          </aside>
        )}
        <main className="flex-1 overflow-hidden">{children}</main>
      </div>
      {sidebar && (
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetContent side="left" className="w-72 p-0">
            {sidebar}
          </SheetContent>
        </Sheet>
      )}
    </div>
  );
}
