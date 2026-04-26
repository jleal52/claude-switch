import { useSessionMessages } from '@/api/hooks';

export function Transcript({ sessionID, visible }: { sessionID: string; visible: boolean }) {
  const messages = useSessionMessages(sessionID, visible);
  if (!visible) return null;

  return (
    <aside className="w-96 shrink-0 overflow-y-auto border-l bg-background p-3 text-sm">
      <h3 className="mb-2 font-medium">Transcript</h3>
      {messages.isLoading && <div className="text-muted-foreground">Loading…</div>}
      {messages.error && <div className="text-destructive">Failed to load</div>}
      {(messages.data ?? []).length === 0 && !messages.isLoading && (
        <div className="text-muted-foreground">No transcript stored.</div>
      )}
      <ul className="space-y-2">
        {(messages.data ?? []).map((m, i) => (
          <li key={i} className="rounded border bg-muted/40 p-2 font-mono text-xs">
            <div className="mb-1 text-muted-foreground">{m.ts}</div>
            <pre className="whitespace-pre-wrap break-words">{m.entry}</pre>
          </li>
        ))}
      </ul>
    </aside>
  );
}
