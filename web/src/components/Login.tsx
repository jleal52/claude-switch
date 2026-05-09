import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { Button } from '@/components/ui/button';

interface ProvidersResponse {
  providers_configured: string[];
}

function GithubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true" className={className} fill="currentColor">
      <path d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

const SPECS: ReadonlyArray<readonly [string, string]> = [
  ['transport', 'one outbound ws'],
  ['frames', 'json envelope · binary pty'],
  ['coalesce', '16 ms / 16 KiB'],
  ['replay', '32 KiB ring buffer'],
  ['pty', 'posix · conpty'],
  ['runtime', 'your machine, your claude'],
];

export function Login() {
  const { data } = useQuery({
    queryKey: ['providers'],
    queryFn: () => apiClient<ProvidersResponse>('/api/me').catch(() => ({ providers_configured: ['github'] })),
    staleTime: 5 * 60_000,
  });

  const providers = data?.providers_configured ?? [];

  return (
    <main className="grid min-h-screen place-items-center bg-background p-6">
      <div className="w-full max-w-md space-y-6 rounded-lg border bg-card p-8 shadow">
        <header className="space-y-2">
          <p className="font-mono text-xs uppercase tracking-[0.2em] text-muted-foreground">
            $ claude-switch --remote
          </p>
          <h1 className="text-xl font-semibold">Sign in to claude-switch</h1>
          <p className="text-sm text-muted-foreground">
            Drive your local <code className="font-mono">claude</code> REPL from any browser. The
            wrapper opens a single outbound WebSocket; the server fans out PTY frames to the tabs
            you trust.
          </p>
        </header>

        <div className="flex flex-col gap-2">
          {providers.includes('github') && (
            <Button asChild variant="outline">
              <a href="/auth/github/login">
                <GithubMark className="mr-2 h-4 w-4" />
                Continue with GitHub
              </a>
            </Button>
          )}
        </div>

        <section
          aria-label="Technical specs"
          className="rounded-md border bg-muted/40 p-4 font-mono text-xs leading-relaxed"
        >
          <p className="mb-2 text-muted-foreground">{'// spec sheet'}</p>
          <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-1">
            {SPECS.map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-muted-foreground">{k}</dt>
                <dd className="text-foreground">{v}</dd>
              </div>
            ))}
          </dl>
          <p className="mt-3 text-muted-foreground">
            {'> '}
            <span className="text-foreground">the relay shovels bytes. the model lives on your laptop.</span>
            <span className="ml-1 inline-block w-2 animate-pulse bg-foreground align-[-1px]">&nbsp;</span>
          </p>
        </section>
      </div>
    </main>
  );
}
