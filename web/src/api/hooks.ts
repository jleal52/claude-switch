import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from './client';

// MeResponse mirrors the server's /api/me JSON shape (snake_case fields).
// We define this here rather than reusing the tygo-generated User struct
// because the server's MeHandlers wraps it in {user: {…}, providers_configured}.
export interface MeUser {
  id: string;
  email?: string;
  name?: string;
  avatar_url?: string;
  keep_transcripts: boolean;
  transcript_retention_days?: number;
}

export interface MeResponse {
  user: MeUser;
  providers_configured: string[];
}

export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => apiClient<MeResponse>('/api/me'),
    staleTime: 5 * 60_000,
  });
}

export interface WrapperJSON {
  id: string;
  name: string;
  os: string;
  arch: string;
  version: string;
  paired_at: string;
  last_seen_at: string;
  revoked: boolean;
  online: boolean;
}

export function useWrappers() {
  return useQuery({
    queryKey: ['wrappers'],
    queryFn: () => apiClient<WrapperJSON[]>('/api/wrappers'),
    staleTime: 5_000,
    refetchInterval: 10_000,
    refetchOnWindowFocus: true,
  });
}

export interface SessionJSON {
  id: string;
  wrapper_id: string;
  jsonl_uuid?: string;
  cwd: string;
  account: string;
  status: 'starting' | 'running' | 'exited' | 'wrapper_offline';
  created_at: string;
  exited_at?: string;
  exit_code?: number;
  exit_reason?: string;
}

export function useSessions(statusFilter: 'live' | 'all' = 'live') {
  return useQuery({
    queryKey: ['sessions', statusFilter],
    queryFn: () => apiClient<SessionJSON[]>('/api/sessions', { query: { status: statusFilter } }),
    staleTime: 10_000,
  });
}

export interface CreateSessionInput {
  wrapper_id: string;
  cwd: string;
  account?: string;
  args?: string[];
}

export function useCreateSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateSessionInput) =>
      apiClient<SessionJSON>('/api/sessions', { method: 'POST', body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

// useResumeTranscript spawns `claude --resume <jsonl_uuid>` on the
// wrapper that owns the transcript. The server resolves wrapper + cwd
// from the catalog so the portal only has to send the uuid.
export function useResumeTranscript() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (jsonl_uuid: string) =>
      apiClient<SessionJSON>('/api/sessions/resume', { method: 'POST', body: { jsonl_uuid } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

export interface PairRedeemInput { code: string; deny?: boolean }
export interface PairRedeemResult { name: string; os: string; arch: string; version: string }

export function useRedeemPair() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: PairRedeemInput) =>
      apiClient<PairRedeemResult>('/api/pair/redeem', { method: 'POST', body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['wrappers'] });
    },
  });
}

export interface MessageJSON { ts: string; entry: string }

export function useSessionMessages(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['session', id, 'messages'],
    queryFn: () => apiClient<MessageJSON[]>(`/api/sessions/${id}/messages`),
    enabled,
    staleTime: 10_000,
  });
}

export interface SettingsInput {
  keep_transcripts?: boolean;
  transcript_retention_days?: number;
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SettingsInput) =>
      apiClient<void>('/api/me/settings', { method: 'POST', body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['me'] });
    },
  });
}

// === Transcripts catalog + search ===

export interface ProjectJSON {
  id: string;
  wrapper_id: string;
  slug: string;
  cwd: string;
  name: string;
  session_count: number;
  first_activity_at: string;
  last_activity_at: string;
}

export interface TranscriptJSON {
  id: string;
  wrapper_id: string;
  project_id: string;
  jsonl_uuid: string;
  path: string;
  started_at: string;
  ended_at: string;
  message_count: number;
  title: string;
  bytes: number;
}

export function useProjects(wrapperID?: string) {
  return useQuery({
    queryKey: ['projects', wrapperID ?? 'all'],
    queryFn: () =>
      apiClient<ProjectJSON[]>(
        '/api/projects',
        wrapperID ? { query: { wrapper_id: wrapperID } } : {},
      ),
    staleTime: 30_000,
  });
}

export interface TranscriptsListOpts {
  wrapperID?: string;
  projectID?: string;
  limit?: number;
}

export function useTranscripts(opts: TranscriptsListOpts = {}) {
  const query: Record<string, string | number> = {};
  if (opts.wrapperID) query.wrapper_id = opts.wrapperID;
  if (opts.projectID) query.project_id = opts.projectID;
  if (opts.limit) query.limit = opts.limit;
  return useQuery({
    queryKey: ['transcripts', opts.wrapperID ?? 'any', opts.projectID ?? 'any', opts.limit ?? 200],
    queryFn: () => apiClient<TranscriptJSON[]>('/api/transcripts', { query }),
    staleTime: 15_000,
  });
}

export interface SearchMatchJSON {
  transcript_id: string; // wrapper-side jsonl_uuid (per proto)
  msg_index: number;
  role: string;
  snippet: string;
  ts?: string;
}

export interface WrapperSearchStatus {
  Status: 'ok' | 'offline' | 'timeout' | 'error';
  Count?: number;
  ElapsedMs?: number;
  Error?: string;
}

export interface SearchResponseJSON {
  matches: SearchMatchJSON[];
  by_wrapper: Record<string, WrapperSearchStatus>;
}

export interface SearchInput {
  query: string;
  project_id?: string;        // slug, wrapper-side
  wrapper_ids?: string[];
  transcript_ids?: string[];  // jsonl_uuids
  max_results?: number;
  case_insensitive?: boolean;
}

export function useSearch() {
  return useMutation({
    mutationFn: (input: SearchInput) =>
      apiClient<SearchResponseJSON>('/api/search', { method: 'POST', body: input }),
  });
}

export function useDeleteTranscript() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiClient<void>(`/api/transcripts/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['transcripts'] });
    },
  });
}
