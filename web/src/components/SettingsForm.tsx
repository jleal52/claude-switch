import { useState, useEffect } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useMe, useUpdateSettings } from '@/api/hooks';
import { useToast } from '@/components/ui/use-toast';

export function SettingsForm() {
  const me = useMe();
  const update = useUpdateSettings();
  const { toast } = useToast();
  const [keep, setKeep] = useState(false);
  const [days, setDays] = useState(30);

  useEffect(() => {
    if (me.data) {
      setKeep(me.data.user.keep_transcripts);
      const t = me.data.user.transcript_retention_days;
      if (typeof t === 'number' && t > 0) setDays(t);
    }
  }, [me.data]);

  async function save() {
    try {
      await update.mutateAsync({
        keep_transcripts: keep,
        transcript_retention_days: keep ? Math.min(90, Math.max(1, days)) : undefined,
      });
      toast({ title: 'Settings saved' });
    } catch (e) {
      toast({ title: 'Could not save', description: String(e), variant: 'destructive' });
    }
  }

  return (
    <div className="mx-auto mt-8 max-w-xl space-y-6 rounded-lg border bg-card p-6 shadow">
      <h1 className="text-xl font-semibold">Settings</h1>
      {me.data && (
        <p className="text-sm text-muted-foreground">
          Signed in as {me.data.user.email ?? me.data.user.id}
        </p>
      )}

      <section className="space-y-3">
        <h2 className="font-medium">Transcripts</h2>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={keep}
            onChange={(e) => setKeep(e.target.checked)}
          />
          Keep transcripts of my sessions on the server
        </label>
        <label className="flex items-center gap-2 text-sm">
          Retention (days):
          <Input
            type="number"
            min={1}
            max={90}
            value={days}
            disabled={!keep}
            onChange={(e) => setDays(Number(e.target.value))}
            className="w-24"
          />
        </label>
      </section>

      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={() => location.reload()}>Discard</Button>
        <Button onClick={save} disabled={update.isPending}>
          {update.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  );
}
