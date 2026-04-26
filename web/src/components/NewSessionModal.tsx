import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogTrigger,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useCreateSession, useWrappers } from '@/api/hooks';
import { useToast } from '@/components/ui/use-toast';

export function NewSessionModal({ defaultWrapperID, trigger }: {
  defaultWrapperID?: string;
  trigger: React.ReactNode;
}) {
  const wrappers = useWrappers();
  const create = useCreateSession();
  const nav = useNavigate();
  const { toast } = useToast();
  const [open, setOpen] = useState(false);
  const [wrapperID, setWrapperID] = useState(defaultWrapperID ?? '');
  const [cwd, setCwd] = useState('');

  async function submit() {
    try {
      const s = await create.mutateAsync({ wrapper_id: wrapperID, cwd, account: 'default' });
      setOpen(false);
      nav({ to: '/sessions/$id', params: { id: s.id } });
    } catch (e) {
      toast({ title: 'Could not create session', description: String(e), variant: 'destructive' });
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New session</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <label className="block text-sm">
            Wrapper
            <select
              className="mt-1 w-full rounded border bg-background p-2 text-sm"
              value={wrapperID}
              onChange={(e) => setWrapperID(e.target.value)}
            >
              <option value="" disabled>Select wrapper…</option>
              {(wrappers.data ?? []).map((w) => (
                <option key={w.id} value={w.id}>{w.name} ({w.os}/{w.arch})</option>
              ))}
            </select>
          </label>
          <label className="block text-sm">
            Working directory
            <Input className="mt-1" placeholder="/home/user" value={cwd} onChange={(e) => setCwd(e.target.value)} />
          </label>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
          <Button onClick={submit} disabled={!wrapperID || !cwd || create.isPending}>
            {create.isPending ? 'Creating…' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
