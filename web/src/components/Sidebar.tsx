import { Link } from '@tanstack/react-router';
import { useWrappers, useSessions, type WrapperJSON, type SessionJSON } from '@/api/hooks';
import { Button } from '@/components/ui/button';
import { Plus } from 'lucide-react';
import { NewSessionModal } from './NewSessionModal';

export function Sidebar() {
  const wrappers = useWrappers();
  const sessions = useSessions('live');

  if (wrappers.isLoading || sessions.isLoading) {
    return <div className="p-3 text-sm text-muted-foreground">Loading…</div>;
  }
  if (wrappers.error || sessions.error) {
    return <div className="p-3 text-sm text-destructive">Failed to load.</div>;
  }
  const sessionsByWrapper = new Map<string, SessionJSON[]>();
  for (const s of sessions.data ?? []) {
    const arr = sessionsByWrapper.get(s.wrapper_id) ?? [];
    arr.push(s);
    sessionsByWrapper.set(s.wrapper_id, arr);
  }

  return (
    <nav className="flex h-full flex-col gap-2 overflow-y-auto p-3 text-sm">
      {(wrappers.data ?? []).length === 0 && (
        <div className="rounded-md border border-dashed p-3 text-muted-foreground">
          No wrappers paired. <Link to="/pair" className="underline">Pair one</Link>.
        </div>
      )}
      {(wrappers.data ?? []).map((w: WrapperJSON) => (
        <section key={w.id} className="space-y-1">
          <header className="flex items-center justify-between gap-2">
            <span className="flex min-w-0 items-center gap-2">
              <span
                className={
                  w.online
                    ? 'inline-block h-2.5 w-2.5 shrink-0 rounded-full bg-emerald-500 shadow-[0_0_0_3px_rgba(16,185,129,0.22)]'
                    : 'inline-block h-2.5 w-2.5 shrink-0 rounded-full bg-red-500 shadow-[0_0_0_3px_rgba(239,68,68,0.20)]'
                }
                role="status"
                aria-label={w.online ? 'wrapper online' : 'wrapper offline'}
                title={w.online ? 'Conectado' : 'Desconectado'}
              />
              <span className="truncate font-medium">{w.name}</span>
              <span
                className={
                  w.online
                    ? 'shrink-0 font-mono text-[10px] uppercase tracking-wider text-emerald-500'
                    : 'shrink-0 font-mono text-[10px] uppercase tracking-wider text-red-500'
                }
              >
                {w.online ? 'online' : 'offline'}
              </span>
            </span>
            <span className="shrink-0 text-xs text-muted-foreground">{w.os}/{w.arch}</span>
          </header>
          <ul className="space-y-0.5 pl-2">
            {(sessionsByWrapper.get(w.id) ?? []).map((s) => (
              <li key={s.id}>
                <Link
                  to="/sessions/$id"
                  params={{ id: s.id }}
                  className="block truncate rounded px-2 py-1 hover:bg-accent"
                  activeProps={{ className: 'bg-accent' }}
                >
                  <span className={statusDot(s.status)} aria-hidden /> {s.cwd}
                </Link>
              </li>
            ))}
          </ul>
          <NewSessionModal
            defaultWrapperID={w.id}
            trigger={
              <Button variant="ghost" size="sm" className="w-full justify-start text-muted-foreground">
                <Plus className="mr-1 h-3 w-3" /> Nueva sesión
              </Button>
            }
          />
        </section>
      ))}
      <div className="mt-auto border-t pt-2">
        <Button asChild variant="outline" size="sm" className="w-full">
          <Link to="/pair">Pair a wrapper</Link>
        </Button>
      </div>
    </nav>
  );
}

function statusDot(status: SessionJSON['status']): string {
  switch (status) {
    case 'running':         return 'mr-2 inline-block h-2 w-2 rounded-full bg-green-500';
    case 'starting':        return 'mr-2 inline-block h-2 w-2 rounded-full bg-yellow-500';
    case 'wrapper_offline': return 'mr-2 inline-block h-2 w-2 rounded-full bg-orange-500';
    case 'exited':          return 'mr-2 inline-block h-2 w-2 rounded-full bg-zinc-400';
  }
}
