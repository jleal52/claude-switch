import { useQuery } from '@tanstack/react-query';
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
