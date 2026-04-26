import { useRef } from 'react';
import { Terminal, type TerminalHandle } from './Terminal';
import { useSessionStream } from '@/hooks/useSessionStream';
import { useToast } from '@/components/ui/use-toast';
import { Button } from '@/components/ui/button';
import { useNavigate } from '@tanstack/react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';

export function SessionView({ sessionID }: { sessionID: string }) {
  const apiRef = useRef<TerminalHandle | null>(null);
  const { toast } = useToast();
  const nav = useNavigate();
  const qc = useQueryClient();
  const closeMut = useMutation({
    mutationFn: () => apiClient(`/api/sessions/${sessionID}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sessions'] });
      nav({ to: '/' });
    },
  });

  const stream = useSessionStream({
    sessionID,
    onData: (bytes) => apiRef.current?.write(bytes),
    onControl: (frame) => {
      switch (frame.type) {
        case 'wrapper.offline':
          toast({ title: 'Wrapper offline', variant: 'destructive' });
          break;
        case 'session.exited': {
          const p = frame.payload as { exit_code?: number; reason?: string } | undefined;
          toast({ title: `Session exited (${p?.exit_code ?? '?'})`, description: p?.reason });
          break;
        }
        // replay.start / replay.end ignored: xterm.js handles bytes without ordering hints.
      }
    },
  });

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b bg-background px-4 py-2">
        <span className="text-sm font-medium">{sessionID}</span>
        <span className="text-xs text-muted-foreground">{stream.status}</span>
        <Button size="sm" variant="outline" onClick={() => closeMut.mutate()}>
          Close session
        </Button>
      </header>
      <div className="flex-1 overflow-hidden bg-[#0b0b10]">
        <Terminal
          apiRef={apiRef}
          onInput={(b) => stream.write(b)}
          onResize={(c, r) => stream.resize(c, r)}
        />
      </div>
    </div>
  );
}
