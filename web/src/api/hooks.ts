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
}

export function useWrappers() {
  return useQuery({
    queryKey: ['wrappers'],
    queryFn: () => apiClient<WrapperJSON[]>('/api/wrappers'),
    staleTime: 30_000,
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
