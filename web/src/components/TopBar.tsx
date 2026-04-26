import { useMe } from '@/api/hooks';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent,
  DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu';
import { Link } from '@tanstack/react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';

export function TopBar() {
  const { data } = useMe();
  const qc = useQueryClient();
  const logout = useMutation({
    mutationFn: () => apiClient('/api/auth/logout', { method: 'POST' }),
    onSuccess: () => {
      qc.clear();
      window.location.href = '/login';
    },
  });

  return (
    <header className="flex h-14 items-center justify-between border-b bg-background px-4">
      <Link to="/" className="text-lg font-semibold">claude-switch</Link>
      <div className="flex items-center gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" aria-label="Open user menu">
              {data?.user.email ?? 'me'}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>
              {data?.user.name || data?.user.email || 'Signed in'}
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to="/settings">Settings</Link>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => logout.mutate()}>
              Logout
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
