import { useMemo, useState } from 'react';
import {
  useProjects,
  useTranscripts,
  useWrappers,
  useSearch,
  useDeleteTranscript,
  type ProjectJSON,
  type TranscriptJSON,
  type WrapperJSON,
} from '@/api/hooks';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

// TranscriptsBrowser is the main pane for the /transcripts route.
//
// Layout: a 240px sidebar listing projects (grouped by wrapper) on the
// left, and a main area showing either:
//   - the list of transcripts for the active scope (no query), or
//   - the search results panel (after running a search).
export function TranscriptsBrowser() {
  const wrappers = useWrappers();
  const projects = useProjects(undefined); // all of user's wrappers
  const [selectedProjectID, setSelectedProjectID] = useState<string | undefined>();
  const [selectedWrapperID, setSelectedWrapperID] = useState<string | undefined>();
  const [query, setQuery] = useState('');
  const [caseInsensitive, setCaseInsensitive] = useState(true);

  const transcripts = useTranscripts({
    projectID: selectedProjectID,
    wrapperID: selectedProjectID ? undefined : selectedWrapperID,
    limit: 200,
  });

  const search = useSearch();

  const wrapperByID = useMemo(() => {
    const m = new Map<string, WrapperJSON>();
    for (const w of wrappers.data ?? []) m.set(w.id, w);
    return m;
  }, [wrappers.data]);

  const projectsByWrapper = useMemo(() => {
    const m = new Map<string, ProjectJSON[]>();
    for (const p of projects.data ?? []) {
      const arr = m.get(p.wrapper_id) ?? [];
      arr.push(p);
      m.set(p.wrapper_id, arr);
    }
    return m;
  }, [projects.data]);

  const runSearch = (e: React.FormEvent) => {
    e.preventDefault();
    if (!query.trim()) return;
    search.mutate({
      query: query.trim(),
      wrapper_ids: selectedWrapperID ? [selectedWrapperID] : undefined,
      project_id: selectedProjectID
        ? (projects.data?.find((p) => p.id === selectedProjectID)?.slug ?? undefined)
        : undefined,
      case_insensitive: caseInsensitive,
      max_results: 200,
    });
  };

  return (
    <div className="grid h-full grid-cols-[260px_1fr] gap-0">
      <aside className="overflow-y-auto border-r p-3 text-sm">
        <h2 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Projects
        </h2>
        <button
          className={
            'mb-2 block w-full rounded px-2 py-1 text-left hover:bg-accent ' +
            (!selectedProjectID && !selectedWrapperID ? 'bg-accent' : '')
          }
          onClick={() => {
            setSelectedProjectID(undefined);
            setSelectedWrapperID(undefined);
          }}
        >
          All wrappers
        </button>
        {(wrappers.data ?? []).map((w) => (
          <section key={w.id} className="mb-3">
            <button
              className={
                'flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs font-semibold uppercase tracking-wider hover:bg-accent ' +
                (selectedWrapperID === w.id && !selectedProjectID ? 'bg-accent' : '')
              }
              onClick={() => {
                setSelectedWrapperID(w.id);
                setSelectedProjectID(undefined);
              }}
            >
              <span className="truncate">{w.name}</span>
              <span
                className={
                  w.online
                    ? 'inline-block h-2 w-2 shrink-0 rounded-full bg-emerald-500'
                    : 'inline-block h-2 w-2 shrink-0 rounded-full bg-red-500'
                }
                title={w.online ? 'online' : 'offline'}
              />
            </button>
            <ul className="mt-1 space-y-0.5 pl-2">
              {(projectsByWrapper.get(w.id) ?? []).map((p) => (
                <li key={p.id}>
                  <button
                    className={
                      'block w-full truncate rounded px-2 py-1 text-left hover:bg-accent ' +
                      (selectedProjectID === p.id ? 'bg-accent' : '')
                    }
                    onClick={() => {
                      setSelectedProjectID(p.id);
                      setSelectedWrapperID(w.id);
                    }}
                    title={p.cwd}
                  >
                    {p.name} <span className="ml-1 text-xs text-muted-foreground">({p.session_count})</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </aside>

      <main className="flex h-full min-h-0 flex-col overflow-hidden">
        <form onSubmit={runSearch} className="flex gap-2 border-b p-3">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Buscar texto en transcripciones…"
          />
          <label className="flex items-center gap-1 whitespace-nowrap text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={caseInsensitive}
              onChange={(e) => setCaseInsensitive(e.target.checked)}
            />
            case-insensitive
          </label>
          <Button type="submit" disabled={!query.trim() || search.isPending}>
            {search.isPending ? 'Buscando…' : 'Buscar'}
          </Button>
        </form>

        <div className="flex-1 overflow-y-auto p-3">
          {search.data ? (
            <SearchResultsView
              data={search.data}
              wrapperByID={wrapperByID}
              onClear={() => search.reset()}
            />
          ) : (
            <TranscriptsList rows={transcripts.data ?? []} loading={transcripts.isLoading} />
          )}
        </div>
      </main>
    </div>
  );
}

function TranscriptsList({ rows, loading }: { rows: TranscriptJSON[]; loading: boolean }) {
  const del = useDeleteTranscript();
  if (loading) return <div className="text-muted-foreground">Cargando…</div>;
  if (rows.length === 0) return <div className="text-muted-foreground">Sin transcripciones todavía.</div>;
  const onDelete = (t: TranscriptJSON) => {
    const label = t.title?.slice(0, 60) || t.jsonl_uuid.slice(0, 8);
    if (!confirm(`¿Eliminar la conversación "${label}"? Se ocultará del portal; el archivo en disco no se toca.`)) return;
    del.mutate(t.id);
  };
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs uppercase tracking-wider text-muted-foreground">
        <tr>
          <th className="py-2">Título</th>
          <th className="py-2">Inicio</th>
          <th className="py-2">Mensajes</th>
          <th className="py-2">Tamaño</th>
          <th className="py-2 w-10"></th>
        </tr>
      </thead>
      <tbody>
        {rows.map((t) => (
          <tr key={t.id} className="border-t">
            <td className="max-w-md truncate py-2" title={t.title}>
              {t.title || <span className="text-muted-foreground">(sin título)</span>}
            </td>
            <td className="py-2 font-mono text-xs">{t.started_at.slice(0, 19).replace('T', ' ')}</td>
            <td className="py-2">{t.message_count}</td>
            <td className="py-2 text-muted-foreground">{formatBytes(t.bytes)}</td>
            <td className="py-2 text-right">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onDelete(t)}
                disabled={del.isPending}
                title="Eliminar conversación del portal"
              >
                ✕
              </Button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function SearchResultsView({
  data,
  wrapperByID,
  onClear,
}: {
  data: NonNullable<ReturnType<typeof useSearch>['data']>;
  wrapperByID: Map<string, WrapperJSON>;
  onClear: () => void;
}) {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-sm">
          <span className="font-semibold">{data.matches.length}</span> coincidencias
        </span>
        <Button variant="ghost" size="sm" onClick={onClear}>
          Volver al catálogo
        </Button>
      </div>

      <div className="rounded-md border p-3 text-xs">
        <div className="mb-2 font-semibold uppercase tracking-wider text-muted-foreground">
          Estado por wrapper
        </div>
        <ul className="space-y-1">
          {Object.entries(data.by_wrapper).map(([wid, s]) => (
            <li key={wid} className="flex items-center justify-between">
              <span>{wrapperByID.get(wid)?.name ?? wid}</span>
              <span
                className={
                  s.Status === 'ok'
                    ? 'text-emerald-500'
                    : s.Status === 'offline'
                    ? 'text-red-500'
                    : 'text-amber-500'
                }
              >
                {s.Status}
                {typeof s.Count === 'number' ? ` · ${s.Count}` : ''}
                {typeof s.ElapsedMs === 'number' ? ` · ${s.ElapsedMs}ms` : ''}
              </span>
            </li>
          ))}
        </ul>
      </div>

      <ul className="space-y-2">
        {data.matches.map((m, i) => (
          <li key={`${m.transcript_id}-${m.msg_index}-${i}`} className="rounded-md border p-3 text-sm">
            <div className="mb-1 flex items-center justify-between text-xs text-muted-foreground">
              <span className="font-mono">{m.transcript_id.slice(0, 8)}…</span>
              <span>
                {m.role} · #{m.msg_index}
                {m.ts ? ` · ${m.ts.slice(0, 19).replace('T', ' ')}` : ''}
              </span>
            </div>
            <pre className="whitespace-pre-wrap break-words text-sm">{m.snippet}</pre>
          </li>
        ))}
      </ul>
    </div>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
