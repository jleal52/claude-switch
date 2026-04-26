import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useRedeemPair } from '@/api/hooks';
import { useToast } from '@/components/ui/use-toast';

export function PairForm() {
  const [code, setCode] = useState('');
  const redeem = useRedeemPair();
  const { toast } = useToast();
  const [paired, setPaired] = useState<{ name: string; os: string; arch: string } | null>(null);

  async function approve() {
    try {
      const w = await redeem.mutateAsync({ code: code.trim().toUpperCase() });
      setPaired({ name: w.name, os: w.os, arch: w.arch });
    } catch (e) {
      toast({ title: 'Pairing failed', description: String(e), variant: 'destructive' });
    }
  }

  if (paired) {
    return (
      <div className="mx-auto mt-12 max-w-md rounded-lg border bg-card p-6 shadow space-y-3">
        <h1 className="text-xl font-semibold">Paired ✓</h1>
        <p className="text-sm">
          {paired.name} ({paired.os}/{paired.arch}) is now bound to your account.
        </p>
        <Button asChild variant="outline">
          <Link to="/">Back to dashboard</Link>
        </Button>
      </div>
    );
  }

  return (
    <div className="mx-auto mt-12 max-w-md rounded-lg border bg-card p-6 shadow space-y-3">
      <h1 className="text-xl font-semibold">Pair a wrapper</h1>
      <p className="text-sm text-muted-foreground">
        On the machine you want to pair, run: <code className="rounded bg-muted px-1">claude-switch pair https://&lt;this-host&gt;</code>
      </p>
      <Input
        placeholder="ABCD-1234"
        value={code}
        onChange={(e) => setCode(e.target.value)}
        autoFocus
      />
      <div className="flex justify-end gap-2">
        <Button asChild variant="ghost">
          <Link to="/">Cancel</Link>
        </Button>
        <Button onClick={approve} disabled={!code || redeem.isPending}>
          {redeem.isPending ? 'Approving…' : 'Approve'}
        </Button>
      </div>
    </div>
  );
}
