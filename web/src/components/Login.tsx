import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { Button } from '@/components/ui/button';
import { GitBranch } from 'lucide-react';

interface ProvidersResponse {
  providers_configured: string[];
}

export function Login() {
  const { data } = useQuery({
    queryKey: ['providers'],
    queryFn: () => apiClient<ProvidersResponse>('/api/me').catch(() => ({ providers_configured: ['github', 'google'] })),
    staleTime: 5 * 60_000,
  });

  const providers = data?.providers_configured ?? [];

  return (
    <main className="grid min-h-screen place-items-center bg-background">
      <div className="w-full max-w-sm space-y-4 rounded-lg border bg-card p-8 shadow">
        <h1 className="text-xl font-semibold">Sign in to claude-switch</h1>
        <div className="flex flex-col gap-2">
          {providers.includes('github') && (
            <Button asChild variant="outline">
              <a href="/auth/github/login">
                <GitBranch className="mr-2 h-4 w-4" />
                Continue with GitHub
              </a>
            </Button>
          )}
          {providers.includes('google') && (
            <Button asChild variant="outline">
              <a href="/auth/google/login">
                <span className="mr-2 inline-block h-4 w-4 rounded-full bg-[conic-gradient(from_180deg,#ea4335,#fbbc05,#34a853,#4285f4,#ea4335)]" />
                Continue with Google
              </a>
            </Button>
          )}
        </div>
      </div>
    </main>
  );
}
