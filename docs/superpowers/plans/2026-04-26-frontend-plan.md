# Frontend (subsystem 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the React + TypeScript SPA at `web/` that fronts `claude-switch-server`'s REST and WebSocket APIs. Logged-in users sign in via OAuth, see a sidebar of paired wrappers and live sessions, open a session in an xterm.js pane, redeem pairing codes, and toggle transcript storage.

**Architecture:** Vite-built single-page app, same-origin with the server (no CORS). TanStack Query owns server data and cache; TanStack Router owns navigation. Auth is via the server's session cookie (set during OAuth callback) and a CSRF cookie mirrored into request headers. xterm.js renders raw PTY bytes streamed over a per-session WebSocket. The bundle lives at `web/dist/` and is embedded into the server binary via `go:embed all:../../web/dist`.

**Tech Stack:** React 18, Vite 5, TypeScript (strict), Tailwind CSS, shadcn/ui, TanStack Query v5, TanStack Router v1, xterm.js + addons (fit / web-links / search), Vitest + Testing Library + MSW + mock-socket, Playwright for e2e, `tygo` for Go→TS type codegen.

**Spec:** `docs/superpowers/specs/2026-04-26-frontend-design.md` — read it first; this plan implements what that doc specifies.

**Subsystem 2 carryover:** Server endpoints (`/api/*`, `/auth/*`, `/device/*`, `/ws/*`), cookie names (`cs_session`, `cs_csrf`, `cs_oauth_state`), CSRF semantics (double-submit + WS query `?ct=`), and the binary `pty.data` framing (1-byte version + 16-byte ULID + payload) are all already implemented and tested. Don't re-invent.

---

## Task 1: Bootstrap web/ package + Vite + Tailwind

**Goal:** A minimal Vite + React + TS project at `web/` that builds and serves "Hello".

**Files:**
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/tsconfig.node.json`
- Create: `web/vite.config.ts`
- Create: `web/index.html`
- Create: `web/postcss.config.cjs`
- Create: `web/tailwind.config.ts`
- Create: `web/src/main.tsx`
- Create: `web/src/styles/globals.css`
- Create: `web/.gitignore`

- [ ] **Step 1: Scaffold the package**

Run from the repo root:

```bash
mkdir -p web/src/styles
cat > web/package.json <<'EOF'
{
  "name": "claude-switch-web",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "tsc --noEmit"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.3",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.47",
    "tailwindcss": "^3.4.14",
    "typescript": "^5.6.3",
    "vite": "^5.4.10"
  }
}
EOF
```

- [ ] **Step 2: TypeScript config**

`web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "strict": true,
    "skipLibCheck": true,
    "esModuleInterop": true,
    "allowSyntheticDefaultImports": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

`web/tsconfig.node.json`:

```json
{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "allowSyntheticDefaultImports": true,
    "strict": true
  },
  "include": ["vite.config.ts", "tailwind.config.ts", "postcss.config.cjs"]
}
```

- [ ] **Step 3: Vite config with dev proxy**

`web/vite.config.ts`:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: {
    port: 5173,
    proxy: {
      '/api':    { target: 'http://localhost:8080', changeOrigin: true },
      '/auth':   { target: 'http://localhost:8080', changeOrigin: true },
      '/device': { target: 'http://localhost:8080', changeOrigin: true },
      '/ws':     { target: 'ws://localhost:8080', ws: true, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
```

- [ ] **Step 4: HTML entry + main**

`web/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" href="data:," />
    <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
    <title>claude-switch</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`web/src/main.tsx`:

```tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import './styles/globals.css';

function App() {
  return <h1 className="p-4 text-2xl font-bold">claude-switch</h1>;
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

- [ ] **Step 5: Tailwind config + global CSS**

`web/postcss.config.cjs`:

```js
module.exports = {
  plugins: { tailwindcss: {}, autoprefixer: {} },
};
```

`web/tailwind.config.ts`:

```ts
import type { Config } from 'tailwindcss';

export default {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: { extend: {} },
  plugins: [],
} satisfies Config;
```

`web/src/styles/globals.css`:

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

html, body, #root { height: 100%; margin: 0; }
body { font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
```

- [ ] **Step 6: gitignore**

`web/.gitignore`:

```
node_modules/
dist/
.vite/
*.log
.DS_Store
```

- [ ] **Step 7: Install + verify**

```bash
cd web
npm install
npm run build
```

Expected: `web/dist/index.html` and `web/dist/assets/*.js` exist; build prints "✓ built in <Xms>". `npm run lint` (alias for `tsc --noEmit`) passes.

- [ ] **Step 8: Commit**

```bash
git add web tygo.yaml 2>/dev/null; git add web
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): bootstrap React + Vite + TS + Tailwind"
```

---

## Task 2: shadcn/ui setup + base components

**Goal:** Install shadcn/ui's primitives we'll use (`button`, `input`, `dialog`, `toast`, `dropdown-menu`, `tooltip`), with the default theme variables.

**Files:**
- Create: `web/components.json`
- Create: `web/src/lib/utils.ts`
- Create/modify: `web/src/styles/globals.css` (add CSS variables)
- Create: `web/src/components/ui/button.tsx`, `input.tsx`, `dialog.tsx`, `toaster.tsx`, `toast.tsx`, `dropdown-menu.tsx`, `tooltip.tsx`
- Modify: `web/package.json` (add shadcn deps)

- [ ] **Step 1: Add shadcn dependencies**

```bash
cd web
npm install class-variance-authority clsx tailwind-merge \
  @radix-ui/react-dialog @radix-ui/react-dropdown-menu \
  @radix-ui/react-toast @radix-ui/react-tooltip lucide-react
npm install -D tailwindcss-animate
```

- [ ] **Step 2: Tailwind animate plugin**

Update `web/tailwind.config.ts` to include `tailwindcss-animate` and the standard shadcn theme:

```ts
import type { Config } from 'tailwindcss';
import animate from 'tailwindcss-animate';

export default {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
    },
  },
  plugins: [animate],
} satisfies Config;
```

- [ ] **Step 3: CSS variables in globals.css**

Replace `web/src/styles/globals.css`:

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 240 10% 3.9%;
    --primary: 240 5.9% 10%;
    --primary-foreground: 0 0% 98%;
    --secondary: 240 4.8% 95.9%;
    --secondary-foreground: 240 5.9% 10%;
    --muted: 240 4.8% 95.9%;
    --muted-foreground: 240 3.8% 46.1%;
    --accent: 240 4.8% 95.9%;
    --accent-foreground: 240 5.9% 10%;
    --destructive: 0 72% 51%;
    --destructive-foreground: 0 0% 98%;
    --border: 240 5.9% 90%;
    --input: 240 5.9% 90%;
    --ring: 240 5% 64.9%;
    --radius: 0.5rem;
  }
  .dark {
    --background: 240 10% 3.9%;
    --foreground: 0 0% 98%;
    --primary: 0 0% 98%;
    --primary-foreground: 240 5.9% 10%;
    --secondary: 240 3.7% 15.9%;
    --secondary-foreground: 0 0% 98%;
    --muted: 240 3.7% 15.9%;
    --muted-foreground: 240 5% 64.9%;
    --accent: 240 3.7% 15.9%;
    --accent-foreground: 0 0% 98%;
    --destructive: 0 62.8% 30.6%;
    --destructive-foreground: 0 0% 98%;
    --border: 240 3.7% 15.9%;
    --input: 240 3.7% 15.9%;
    --ring: 240 4.9% 83.9%;
  }
  * { @apply border-border; }
  body { @apply bg-background text-foreground; }
}

html, body, #root { height: 100%; margin: 0; }
body { font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
```

- [ ] **Step 4: shadcn config**

`web/components.json`:

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "default",
  "tailwind": {
    "config": "tailwind.config.ts",
    "css": "src/styles/globals.css",
    "baseColor": "zinc",
    "cssVariables": true
  },
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils"
  }
}
```

- [ ] **Step 5: Utils helper**

`web/src/lib/utils.ts`:

```ts
import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
```

- [ ] **Step 6: Generate the components**

Use `npx shadcn@latest add` for each one. Each command emits a single file under `web/src/components/ui/`:

```bash
cd web
npx shadcn@latest add button input dialog toast dropdown-menu tooltip
```

If the network or version of shadcn is unstable, the equivalent file contents are pulled from `https://ui.shadcn.com/r/styles/default/<name>.json`. The implementer can fall back to copy-paste from those URLs.

After this step, `web/src/components/ui/` should contain `button.tsx`, `input.tsx`, `dialog.tsx`, `toast.tsx`, `toaster.tsx`, `use-toast.ts`, `dropdown-menu.tsx`, `tooltip.tsx`.

- [ ] **Step 7: Verify build**

```bash
npm run build
```

Expected: green; bundle size shown.

- [ ] **Step 8: Commit**

```bash
git add web/components.json web/src/lib web/src/components web/src/styles web/tailwind.config.ts web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): shadcn/ui primitives + Tailwind theme"
```

---

## Task 3: tygo codegen for Go→TS API types

**Goal:** A single command (`make codegen-ts`) regenerates `web/src/api/types.ts` from selected Go structs, so the front and back never drift.

**Files:**
- Create: `tygo.yaml` (repo root)
- Modify: `Makefile`
- Create: `web/src/api/types.ts` (committed output)

- [ ] **Step 1: Install tygo**

`tygo` is a Go binary; install via:

```bash
wsl.exe -d Debian -- bash -lc "go install github.com/gzuidhof/tygo@latest"
```

(The binary lands at `~/go/bin/tygo` in WSL.)

- [ ] **Step 2: Configuration**

`tygo.yaml` (repo root):

```yaml
packages:
  - path: "github.com/jleal52/claude-switch/internal/store"
    type_mappings:
      time.Time: "string"
      bson.ObjectID: "string"
    output_path: "web/src/api/types.ts"
    include_files:
      - "users.go"
      - "wrappers.go"
      - "pairing.go"
      - "sessions.go"
      - "messages.go"
      - "auth_sessions.go"
    exclude_types:
      - "WrappersRepo"
      - "WrapperTokensRepo"
      - "UsersRepo"
      - "PairingRepo"
      - "SessionsRepo"
      - "MessagesRepo"
      - "AuthSessionsRepo"
```

- [ ] **Step 3: Makefile target**

Append to `Makefile`:

```makefile
codegen-ts:
	tygo generate
```

Add `codegen-ts` to the `.PHONY` line.

- [ ] **Step 4: Run + verify**

```bash
wsl.exe -d Debian -- bash -lc "cd /mnt/c/Proyectos/claude-switch && PATH=\$HOME/go/bin:\$PATH make codegen-ts"
cat web/src/api/types.ts | head -40
```

Expected: TS file exists with `export interface User`, `Wrapper`, `PairingCode`, `Session`, `SessionMessage`, `AuthSession`, etc., with snake_case field names matching the BSON tags.

If any field decodes oddly (e.g. `bson.ObjectID` showing through), tweak `type_mappings` and re-run.

- [ ] **Step 5: Commit (including the generated file)**

```bash
git add tygo.yaml Makefile web/src/api/types.ts
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(codegen): tygo generates web/src/api/types.ts from Go structs"
```

---

## Task 4: API client + CSRF cookie reader

**Goal:** A typed `apiClient` that wraps `fetch` with cookie credentials and auto-injects the CSRF header on mutating methods.

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/tests/api/client.test.ts`
- Modify: `web/package.json` (add Vitest + MSW)

- [ ] **Step 1: Add test deps**

```bash
cd web
npm install -D vitest @testing-library/react @testing-library/jest-dom \
  @testing-library/user-event jsdom msw@latest
```

Also add to `web/vite.config.ts` a `test` block:

```ts
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'node:path';

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: { /* ...same as before... */ },
  build:  { /* ...same as before... */ },
  test: {
    environment: 'jsdom',
    setupFiles: ['./tests/setup.ts'],
    globals: true,
  },
});
```

(Replace existing `defineConfig` from `'vite'` with `'vitest/config'` so both server and test config live together.)

`web/tests/setup.ts`:

```ts
import '@testing-library/jest-dom/vitest';
```

- [ ] **Step 2: Write failing test**

`web/tests/api/client.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiClient, ApiError } from '@/api/client';

describe('apiClient', () => {
  beforeEach(() => {
    document.cookie = '';
    vi.restoreAllMocks();
  });

  it('GET attaches credentials', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );
    await apiClient<{ ok: boolean }>('/api/test', { method: 'GET' });
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/test',
      expect.objectContaining({ credentials: 'include' }),
    );
  });

  it('POST adds X-CSRF-Token from cs_csrf cookie', async () => {
    document.cookie = 'cs_csrf=tok-abc';
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(null, { status: 204 }),
    );
    await apiClient('/api/x', { method: 'POST', body: { hi: 1 } });
    const init = fetchSpy.mock.calls[0]![1] as RequestInit;
    const headers = new Headers(init.headers);
    expect(headers.get('X-CSRF-Token')).toBe('tok-abc');
    expect(headers.get('Content-Type')).toBe('application/json');
  });

  it('throws ApiError on non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('nope', { status: 401 }),
    );
    await expect(apiClient('/api/me', { method: 'GET' })).rejects.toBeInstanceOf(ApiError);
    try {
      await apiClient('/api/me', { method: 'GET' });
    } catch (e) {
      expect((e as ApiError).status).toBe(401);
    }
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

```bash
npx vitest run tests/api/client.test.ts
```

Expected: fails — `Cannot find module '@/api/client'`.

- [ ] **Step 4: Implement client.ts**

`web/src/api/client.ts`:

```ts
// Lightweight typed fetch wrapper. Always sends cookies. On mutating
// methods (POST/PUT/PATCH/DELETE), reads the cs_csrf cookie and mirrors
// it into the X-CSRF-Token header per the server's double-submit pattern.
//
// Body objects are JSON-encoded automatically; pass a string/FormData/Blob
// to skip that.

export class ApiError extends Error {
  constructor(public status: number, public body: string) {
    super(`api ${status}: ${body}`);
  }
}

export interface ApiOptions extends Omit<RequestInit, 'body'> {
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
}

const MUTATING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

export async function apiClient<T>(path: string, opts: ApiOptions = {}): Promise<T> {
  const method = (opts.method ?? 'GET').toUpperCase();
  const url = withQuery(path, opts.query);

  const headers = new Headers(opts.headers);
  let body: BodyInit | undefined;
  if (opts.body !== undefined) {
    if (typeof opts.body === 'string' || opts.body instanceof FormData || opts.body instanceof Blob) {
      body = opts.body as BodyInit;
    } else {
      body = JSON.stringify(opts.body);
      if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    }
  }
  if (MUTATING.has(method)) {
    const csrf = readCookie('cs_csrf');
    if (csrf) headers.set('X-CSRF-Token', csrf);
  }

  const resp = await fetch(url, {
    ...opts,
    method,
    headers,
    body,
    credentials: 'include',
  });

  if (!resp.ok) {
    const text = await safeText(resp);
    throw new ApiError(resp.status, text);
  }
  if (resp.status === 204) return undefined as T;
  const ct = resp.headers.get('content-type') ?? '';
  if (ct.includes('application/json')) return (await resp.json()) as T;
  return (await resp.text()) as unknown as T;
}

function readCookie(name: string): string | undefined {
  const target = `${name}=`;
  for (const c of document.cookie.split(';')) {
    const t = c.trim();
    if (t.startsWith(target)) return decodeURIComponent(t.slice(target.length));
  }
  return undefined;
}

function withQuery(path: string, q?: ApiOptions['query']): string {
  if (!q) return path;
  const usp = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined) continue;
    usp.set(k, String(v));
  }
  const qs = usp.toString();
  return qs ? `${path}?${qs}` : path;
}

async function safeText(r: Response): Promise<string> {
  try { return await r.text(); } catch { return ''; }
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
npx vitest run tests/api/client.test.ts
```

Expected: 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/tests/api web/tests/setup.ts web/vite.config.ts web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): typed api client with cookies + CSRF header"
```

---

## Task 5: TanStack Query setup + useMe hook

**Goal:** A `QueryClientProvider` wraps the app; `useMe()` returns `{user, providers_configured}` with TanStack Query caching.

**Files:**
- Create: `web/src/api/queryClient.ts`
- Create: `web/src/api/hooks.ts`
- Create: `web/tests/api/hooks.test.tsx`
- Modify: `web/src/main.tsx` (wrap in provider)

- [ ] **Step 1: Add deps**

```bash
cd web
npm install @tanstack/react-query
npm install -D @tanstack/react-query-devtools
```

- [ ] **Step 2: Write failing test**

`web/tests/api/hooks.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useMe } from '@/api/hooks';

function withQuery(client: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('useMe', () => {
  it('returns user payload from /api/me', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user: { id: 'u1', email: 'a@x', name: 'A', avatar_url: '', keep_transcripts: false },
          providers_configured: ['github'],
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useMe(), { wrapper: withQuery(qc) });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data?.user.id).toBe('u1');
    expect(result.current.data?.providers_configured).toEqual(['github']);
  });
});
```

- [ ] **Step 3: Run to verify it fails**

```bash
npx vitest run tests/api/hooks.test.tsx
```

- [ ] **Step 4: Implement queryClient.ts**

`web/src/api/queryClient.ts`:

```ts
import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error: unknown) => {
        // Don't retry 4xx — those are deterministic.
        const status = (error as { status?: number } | undefined)?.status;
        if (status && status >= 400 && status < 500) return false;
        return failureCount < 2;
      },
      staleTime: 30_000,
    },
  },
});
```

- [ ] **Step 5: Implement hooks.ts**

`web/src/api/hooks.ts`:

```ts
import { useQuery, useMutation, useQueryClient, type UseMutationOptions } from '@tanstack/react-query';
import { apiClient } from './client';
import type { User, Wrapper, Session } from './types';

export interface MeResponse {
  user: User & { id: string; email?: string; name?: string; avatar_url?: string; keep_transcripts: boolean };
  providers_configured: string[];
}

export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => apiClient<MeResponse>('/api/me'),
    staleTime: 5 * 60_000,
  });
}

// Wrappers, sessions, and mutations follow in later tasks; the file's
// import surface is intentionally small here.
```

(Note: `User`, `Wrapper`, `Session` come from the tygo-generated `types.ts`. If those generated names differ from these, adapt.)

- [ ] **Step 6: Wire provider in main.tsx**

Replace `web/src/main.tsx`:

```tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { ReactQueryDevtools } from '@tanstack/react-query-devtools';
import { queryClient } from './api/queryClient';
import './styles/globals.css';

function App() {
  return <h1 className="p-4 text-2xl font-bold">claude-switch</h1>;
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
      {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
    </QueryClientProvider>
  </React.StrictMode>,
);
```

- [ ] **Step 7: Run + commit**

```bash
npx vitest run
git add web/src/api web/tests/api/hooks.test.tsx web/src/main.tsx web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): TanStack Query provider + useMe hook"
```

---

## Task 6: TanStack Router skeleton + auth gate

**Goal:** Code-based router with five routes and an auth gate via `beforeLoad` on protected routes that redirects to `/login`.

**Files:**
- Create: `web/src/routes/__root.tsx`
- Create: `web/src/routes/index.tsx`
- Create: `web/src/routes/login.tsx`
- Create: `web/src/routes/pair.tsx`
- Create: `web/src/routes/sessions.$id.tsx`
- Create: `web/src/routes/settings.tsx`
- Create: `web/src/router.ts`
- Modify: `web/src/main.tsx`

- [ ] **Step 1: Add deps**

```bash
cd web
npm install @tanstack/react-router
```

- [ ] **Step 2: Implement __root.tsx**

`web/src/routes/__root.tsx`:

```tsx
import { createRootRoute, Outlet } from '@tanstack/react-router';
import { Toaster } from '@/components/ui/toaster';

export const Route = createRootRoute({
  component: () => (
    <>
      <Outlet />
      <Toaster />
    </>
  ),
});
```

- [ ] **Step 3: Implement leaf routes (placeholders for now)**

`web/src/routes/login.tsx`:

```tsx
import { createRoute } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/login',
  component: () => (
    <main className="grid min-h-screen place-items-center">
      <div className="space-y-3 rounded-lg border bg-card p-8 text-center shadow">
        <h1 className="text-xl font-semibold">Sign in to claude-switch</h1>
        <p className="text-sm text-muted-foreground">Login providers will appear in Task 7.</p>
      </div>
    </main>
  ),
});
```

`web/src/routes/index.tsx`:

```tsx
import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';

async function ensureAuthed() {
  try {
    await queryClient.fetchQuery({
      queryKey: ['me'],
      queryFn: () => apiClient<MeResponse>('/api/me'),
    });
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      throw redirect({ to: '/login' });
    }
    throw e;
  }
}

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/',
  beforeLoad: ensureAuthed,
  component: () => <div className="p-4">Catalog (Task 9 fills this in)</div>,
});
```

`web/src/routes/pair.tsx`:

```tsx
import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/pair',
  beforeLoad: async () => {
    try {
      await queryClient.fetchQuery({ queryKey: ['me'], queryFn: () => apiClient<MeResponse>('/api/me') });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: '/login' });
      throw e;
    }
  },
  component: () => <div className="p-4">Pair form (Task 12 fills this in)</div>,
});
```

`web/src/routes/sessions.$id.tsx`:

```tsx
import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/sessions/$id',
  beforeLoad: async () => {
    try {
      await queryClient.fetchQuery({ queryKey: ['me'], queryFn: () => apiClient<MeResponse>('/api/me') });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: '/login' });
      throw e;
    }
  },
  component: function SessionRoute() {
    const { id } = Route.useParams();
    return <div className="p-4">Session terminal for {id} (Task 17 fills this in)</div>;
  },
});
```

`web/src/routes/settings.tsx`:

```tsx
import { createRoute, redirect } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { queryClient } from '@/api/queryClient';
import { apiClient, ApiError } from '@/api/client';
import type { MeResponse } from '@/api/hooks';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/settings',
  beforeLoad: async () => {
    try {
      await queryClient.fetchQuery({ queryKey: ['me'], queryFn: () => apiClient<MeResponse>('/api/me') });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: '/login' });
      throw e;
    }
  },
  component: () => <div className="p-4">Settings (Task 13 fills this in)</div>,
});
```

- [ ] **Step 4: Build the router**

`web/src/router.ts`:

```ts
import { createRouter } from '@tanstack/react-router';
import { Route as RootRoute } from './routes/__root';
import { Route as IndexRoute } from './routes/index';
import { Route as LoginRoute } from './routes/login';
import { Route as PairRoute } from './routes/pair';
import { Route as SessionRoute } from './routes/sessions.$id';
import { Route as SettingsRoute } from './routes/settings';

const routeTree = RootRoute.addChildren([
  IndexRoute,
  LoginRoute,
  PairRoute,
  SessionRoute,
  SettingsRoute,
]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
```

- [ ] **Step 5: Wire RouterProvider**

Replace the body of `web/src/main.tsx`:

```tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { ReactQueryDevtools } from '@tanstack/react-query-devtools';
import { RouterProvider } from '@tanstack/react-router';
import { queryClient } from './api/queryClient';
import { router } from './router';
import './styles/globals.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
    </QueryClientProvider>
  </React.StrictMode>,
);
```

- [ ] **Step 6: Build + commit**

```bash
cd web
npm run build
git add web/src/main.tsx web/src/router.ts web/src/routes web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): router skeleton with auth-gated routes"
```

---

## Task 7: Login route with provider buttons

**Goal:** `/login` shows OAuth provider buttons, fetched from the server's `providers_configured` list.

**Files:**
- Create: `web/src/components/Login.tsx`
- Modify: `web/src/routes/login.tsx`
- Create: `web/tests/components/Login.test.tsx`

- [ ] **Step 1: Write failing test**

`web/tests/components/Login.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Login } from '@/components/Login';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<Login />', () => {
  it('renders one button per configured provider', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user: null,
          providers_configured: ['github', 'google'],
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<Login />, { wrapper: withQuery(qc) });
    expect(await screen.findByRole('link', { name: /github/i })).toHaveAttribute('href', '/auth/github/login');
    expect(await screen.findByRole('link', { name: /google/i })).toHaveAttribute('href', '/auth/google/login');
  });
});
```

- [ ] **Step 2: Run to verify failure**

```bash
npx vitest run tests/components/Login.test.tsx
```

- [ ] **Step 3: Implement Login.tsx**

`web/src/components/Login.tsx`:

```tsx
import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { Button } from '@/components/ui/button';
import { Github } from 'lucide-react';

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
                <Github className="mr-2 h-4 w-4" />
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
```

(`/api/me` returns 401 when not logged in, but its body still contains a stub `providers_configured` we use as a fallback. If your server doesn't return that on 401, the catch returns the default array of both. In practice the login screen runs JS even when unauthed, so a separate small endpoint `GET /api/auth/providers` could be added later if needed — out of scope here.)

- [ ] **Step 4: Plug into route**

`web/src/routes/login.tsx`:

```tsx
import { createRoute } from '@tanstack/react-router';
import { Route as RootRoute } from './__root';
import { Login } from '@/components/Login';

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: '/login',
  component: Login,
});
```

- [ ] **Step 5: Run + commit**

```bash
npx vitest run
git add web/src/components/Login.tsx web/src/routes/login.tsx web/tests/components
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): /login with provider buttons"
```

---

## Task 8: AppShell + TopBar with user menu

**Goal:** Persistent layout (TopBar with user info + logout) wraps all authed routes.

**Files:**
- Create: `web/src/components/AppShell.tsx`
- Create: `web/src/components/TopBar.tsx`
- Create: `web/tests/components/TopBar.test.tsx`
- Modify: `web/src/routes/index.tsx`, `pair.tsx`, `sessions.$id.tsx`, `settings.tsx` to render `<AppShell>` around their content.

- [ ] **Step 1: Write failing test**

`web/tests/components/TopBar.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TopBar } from '@/components/TopBar';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<TopBar />', () => {
  it('shows user email and a logout option', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user: { id: 'u1', email: 'a@x', name: 'A', avatar_url: '', keep_transcripts: false },
          providers_configured: ['github'],
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<TopBar />, { wrapper: withQuery(qc) });
    expect(await screen.findByText('a@x')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /open user menu/i }));
    expect(screen.getByRole('menuitem', { name: /logout/i })).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Implement TopBar.tsx**

`web/src/components/TopBar.tsx`:

```tsx
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
```

- [ ] **Step 3: Implement AppShell.tsx**

`web/src/components/AppShell.tsx`:

```tsx
import { TopBar } from './TopBar';
import { ReactNode } from 'react';

export function AppShell({
  sidebar,
  children,
}: {
  sidebar?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex h-screen flex-col">
      <TopBar />
      <div className="flex flex-1 overflow-hidden">
        {sidebar && (
          <aside className="hidden w-72 shrink-0 border-r bg-muted/40 md:block">
            {sidebar}
          </aside>
        )}
        <main className="flex-1 overflow-hidden">{children}</main>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Wire into the protected routes**

Update each of `routes/index.tsx`, `routes/pair.tsx`, `routes/settings.tsx`, `routes/sessions.$id.tsx` to wrap their content in `<AppShell>`:

```tsx
// example: index.tsx
component: () => (
  <AppShell sidebar={null}>
    <div className="p-6">Catalog (Task 9 fills this in)</div>
  </AppShell>
),
```

(Add `import { AppShell } from '@/components/AppShell';` to each route file.)

- [ ] **Step 5: Run + commit**

```bash
npx vitest run
git add web/src/components web/src/routes web/tests/components/TopBar.test.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): AppShell + TopBar with user menu and logout"
```

---

## Task 9: Sidebar — wrappers + sessions list

**Goal:** Sidebar renders paired wrappers; under each wrapper, its live sessions; click a session navigates to `/sessions/:id`.

**Files:**
- Create: `web/src/components/Sidebar.tsx`
- Create: `web/tests/components/Sidebar.test.tsx`
- Modify: `web/src/api/hooks.ts` — add `useWrappers`, `useSessions`.
- Modify: `web/src/routes/index.tsx` — render `<AppShell sidebar={<Sidebar />}>`.

- [ ] **Step 1: Add list hooks**

Append to `web/src/api/hooks.ts`:

```ts
import type { Session, Wrapper } from './types';

interface WrapperJSON {
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

interface SessionJSON {
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

export type { WrapperJSON, SessionJSON };
```

(`Wrapper` and `Session` from `types.ts` are similar but with snake_case BSON conventions; we use a separate JSON shape because the server's response JSON tags omit some store fields.)

- [ ] **Step 2: Write failing test**

`web/tests/components/Sidebar.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { router } from '@/router';
import { Sidebar } from '@/components/Sidebar';

function withProviders(history = createMemoryHistory({ initialEntries: ['/'] })) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const r = createRouter({ ...router.options, history });
  return function W({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <RouterProvider router={r}>{children}</RouterProvider>
      </QueryClientProvider>
    );
  };
}

describe('<Sidebar />', () => {
  it('renders a wrapper with its session', async () => {
    let n = 0;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith('/api/me')) {
        return new Response(JSON.stringify({
          user: { id: 'u1', email: 'a@x', keep_transcripts: false },
          providers_configured: ['github'],
        }), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.endsWith('/api/wrappers')) {
        n++;
        return new Response(JSON.stringify([
          { id: 'w1', name: 'ireland', os: 'linux', arch: 'amd64', version: '0.1', paired_at: '', last_seen_at: '', revoked: false },
        ]), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.includes('/api/sessions')) {
        return new Response(JSON.stringify([
          { id: 's1', wrapper_id: 'w1', cwd: '/tmp', account: 'default', status: 'running', created_at: '' },
        ]), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      return new Response('not mocked', { status: 500 });
    });

    render(<Sidebar />, { wrapper: withProviders() });
    expect(await screen.findByText('ireland')).toBeInTheDocument();
    expect(await screen.findByText(/\/tmp/)).toBeInTheDocument();
    expect(n).toBeGreaterThan(0);
  });
});
```

(The mocked router setup is approximate; if `router.options` isn't accessible, replace with a minimal `createRouter({ routeTree, history })` that imports the route tree directly.)

- [ ] **Step 3: Implement Sidebar.tsx**

`web/src/components/Sidebar.tsx`:

```tsx
import { Link } from '@tanstack/react-router';
import { useWrappers, useSessions, type WrapperJSON, type SessionJSON } from '@/api/hooks';
import { Button } from '@/components/ui/button';
import { Plus } from 'lucide-react';

export function Sidebar() {
  const wrappers = useWrappers();
  const sessions = useSessions('live');

  if (wrappers.isLoading || sessions.isLoading) {
    return <div className="p-3 text-sm text-muted-foreground">Loading…</div>;
  }
  if (wrappers.error || sessions.error) {
    return <div className="p-3 text-sm text-destructive">Failed to load.</div>;
  }
  const sessionsByWrapper = new Map<string, SessionJSON[]>();
  for (const s of sessions.data ?? []) {
    const arr = sessionsByWrapper.get(s.wrapper_id) ?? [];
    arr.push(s);
    sessionsByWrapper.set(s.wrapper_id, arr);
  }

  return (
    <nav className="flex h-full flex-col gap-2 overflow-y-auto p-3 text-sm">
      {(wrappers.data ?? []).length === 0 && (
        <div className="rounded-md border border-dashed p-3 text-muted-foreground">
          No wrappers paired. <Link to="/pair" className="underline">Pair one</Link>.
        </div>
      )}
      {(wrappers.data ?? []).map((w: WrapperJSON) => (
        <section key={w.id} className="space-y-1">
          <header className="flex items-center justify-between">
            <span className="font-medium">{w.name}</span>
            <span className="text-xs text-muted-foreground">{w.os}/{w.arch}</span>
          </header>
          <ul className="space-y-0.5 pl-2">
            {(sessionsByWrapper.get(w.id) ?? []).map((s) => (
              <li key={s.id}>
                <Link
                  to="/sessions/$id"
                  params={{ id: s.id }}
                  className="block truncate rounded px-2 py-1 hover:bg-accent"
                  activeProps={{ className: 'bg-accent' }}
                >
                  <span className={statusDot(s.status)} aria-hidden /> {s.cwd}
                </Link>
              </li>
            ))}
          </ul>
          <Button variant="ghost" size="sm" className="w-full justify-start text-muted-foreground" disabled>
            <Plus className="mr-1 h-3 w-3" /> Nueva sesión (Task 11)
          </Button>
        </section>
      ))}
      <div className="mt-auto border-t pt-2">
        <Button asChild variant="outline" size="sm" className="w-full">
          <Link to="/pair">Pair a wrapper</Link>
        </Button>
      </div>
    </nav>
  );
}

function statusDot(status: SessionJSON['status']): string {
  switch (status) {
    case 'running':         return 'mr-2 inline-block h-2 w-2 rounded-full bg-green-500';
    case 'starting':        return 'mr-2 inline-block h-2 w-2 rounded-full bg-yellow-500';
    case 'wrapper_offline': return 'mr-2 inline-block h-2 w-2 rounded-full bg-orange-500';
    case 'exited':          return 'mr-2 inline-block h-2 w-2 rounded-full bg-zinc-400';
  }
}
```

- [ ] **Step 4: Wire into / route**

Update `web/src/routes/index.tsx`:

```tsx
component: () => (
  <AppShell sidebar={<Sidebar />}>
    <div className="grid h-full place-items-center text-muted-foreground">
      Select a session or pair a wrapper to start.
    </div>
  </AppShell>
),
```

Add `import { Sidebar } from '@/components/Sidebar';`.

- [ ] **Step 5: Run + commit**

```bash
npx vitest run
git add web/src/api/hooks.ts web/src/components/Sidebar.tsx web/src/routes/index.tsx web/tests/components/Sidebar.test.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): Sidebar with wrappers + live sessions"
```

---

## Task 10: NewSessionModal + create mutation

**Goal:** Click "+ Nueva sesión" → modal with wrapper dropdown + cwd input → creates session and navigates to it.

**Files:**
- Create: `web/src/components/NewSessionModal.tsx`
- Modify: `web/src/api/hooks.ts` — add `useCreateSession`.
- Modify: `web/src/components/Sidebar.tsx` — wire button.
- Create: `web/tests/components/NewSessionModal.test.tsx`

- [ ] **Step 1: Add the mutation**

Append to `web/src/api/hooks.ts`:

```ts
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
```

- [ ] **Step 2: Implement modal**

`web/src/components/NewSessionModal.tsx`:

```tsx
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
```

- [ ] **Step 3: Wire button in Sidebar**

In `web/src/components/Sidebar.tsx`, replace the disabled "+ Nueva sesión" button per wrapper with:

```tsx
<NewSessionModal
  defaultWrapperID={w.id}
  trigger={
    <Button variant="ghost" size="sm" className="w-full justify-start text-muted-foreground">
      <Plus className="mr-1 h-3 w-3" /> Nueva sesión
    </Button>
  }
/>
```

(Add `import { NewSessionModal } from './NewSessionModal';`.)

- [ ] **Step 4: Test**

`web/tests/components/NewSessionModal.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { NewSessionModal } from '@/components/NewSessionModal';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<NewSessionModal />', () => {
  it('POSTs /api/sessions with form values', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (url.endsWith('/api/wrappers')) {
        return new Response(JSON.stringify([
          { id: 'w1', name: 'ireland', os: 'linux', arch: 'amd64', version: '0.1', paired_at: '', last_seen_at: '', revoked: false },
        ]), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.endsWith('/api/sessions') && (init?.method ?? 'GET') === 'POST') {
        const body = JSON.parse(String(init?.body ?? '{}'));
        return new Response(JSON.stringify({
          id: 'new-id', wrapper_id: body.wrapper_id, cwd: body.cwd, account: 'default',
          status: 'starting', created_at: '',
        }), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      return new Response('mock-miss', { status: 404 });
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <NewSessionModal defaultWrapperID="w1" trigger={<Button>Open</Button>} />,
      { wrapper: withQuery(qc) },
    );

    await userEvent.click(screen.getByRole('button', { name: 'Open' }));
    await userEvent.type(await screen.findByPlaceholderText('/home/user'), '/tmp');
    await userEvent.click(screen.getByRole('button', { name: /create/i }));

    await waitFor(() => {
      const post = fetchSpy.mock.calls.find(([u, i]) => String(u).endsWith('/api/sessions') && (i as RequestInit | undefined)?.method === 'POST');
      expect(post).toBeTruthy();
    });
  });
});
```

- [ ] **Step 5: Run + commit**

```bash
npx vitest run
git add web/src/api/hooks.ts web/src/components web/tests/components/NewSessionModal.test.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): NewSessionModal + useCreateSession"
```

---

## Task 11: PairForm + /pair route

**Goal:** Authenticated form to enter `ABCD-1234` and approve a pairing code.

**Files:**
- Create: `web/src/components/PairForm.tsx`
- Modify: `web/src/api/hooks.ts` — add `useRedeemPair`.
- Modify: `web/src/routes/pair.tsx` — render PairForm.
- Create: `web/tests/components/PairForm.test.tsx`

- [ ] **Step 1: Add mutation**

Append to `web/src/api/hooks.ts`:

```ts
interface PairRedeemInput { code: string; deny?: boolean }
interface PairRedeemResult { name: string; os: string; arch: string; version: string }

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
```

- [ ] **Step 2: Implement form**

`web/src/components/PairForm.tsx`:

```tsx
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
```

- [ ] **Step 3: Wire route**

`web/src/routes/pair.tsx`:

```tsx
component: () => (
  <AppShell sidebar={<Sidebar />}>
    <PairForm />
  </AppShell>
),
```

Add imports: `import { Sidebar } from '@/components/Sidebar';` and `import { PairForm } from '@/components/PairForm';` and `import { AppShell } from '@/components/AppShell';`.

- [ ] **Step 4: Test**

`web/tests/components/PairForm.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PairForm } from '@/components/PairForm';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<PairForm />', () => {
  it('shows paired confirmation on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ name: 'ireland', os: 'linux', arch: 'amd64', version: '0.1' }),
        { status: 200, headers: { 'content-type': 'application/json' } }),
    );
    document.cookie = 'cs_csrf=tok';

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<PairForm />, { wrapper: withQuery(qc) });
    await userEvent.type(screen.getByPlaceholderText('ABCD-1234'), 'abcd-1234');
    await userEvent.click(screen.getByRole('button', { name: /approve/i }));

    expect(await screen.findByText(/Paired/)).toBeInTheDocument();
    expect(await screen.findByText(/ireland/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 5: Run + commit**

```bash
npx vitest run
git add web/src/api/hooks.ts web/src/components/PairForm.tsx web/src/routes/pair.tsx web/tests/components/PairForm.test.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): /pair form with redeem mutation"
```

---

## Task 12: SettingsForm + /settings route

**Goal:** Form to toggle `keep_transcripts` and set `transcript_retention_days` (1-90), plus logout button.

**Files:**
- Create: `web/src/components/SettingsForm.tsx`
- Modify: `web/src/api/hooks.ts` — add `useUpdateSettings`.
- Modify: `web/src/routes/settings.tsx` — render SettingsForm.
- Create: `web/tests/components/SettingsForm.test.tsx`

- [ ] **Step 1: Add mutation**

Append to `web/src/api/hooks.ts`:

```ts
interface SettingsInput {
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
```

- [ ] **Step 2: Implement form**

`web/src/components/SettingsForm.tsx`:

```tsx
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
      // transcript_retention_days isn't on the User type by default; cast.
      const t = (me.data.user as { transcript_retention_days?: number }).transcript_retention_days;
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
```

- [ ] **Step 3: Wire route**

`web/src/routes/settings.tsx`:

```tsx
component: () => (
  <AppShell sidebar={<Sidebar />}>
    <SettingsForm />
  </AppShell>
),
```

Add `import { Sidebar } from '@/components/Sidebar';`, `import { SettingsForm } from '@/components/SettingsForm';`, `import { AppShell } from '@/components/AppShell';`.

- [ ] **Step 4: Test**

`web/tests/components/SettingsForm.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SettingsForm } from '@/components/SettingsForm';

function withQuery(client: QueryClient) {
  return function W({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('<SettingsForm />', () => {
  it('clamps retention to 1-90 and POSTs settings', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input);
      if (url.endsWith('/api/me') && (init?.method ?? 'GET') === 'GET') {
        return new Response(JSON.stringify({
          user: { id: 'u1', email: 'a@x', keep_transcripts: false },
          providers_configured: ['github'],
        }), { status: 200, headers: { 'content-type': 'application/json' } });
      }
      if (url.endsWith('/api/me/settings') && init?.method === 'POST') {
        return new Response(null, { status: 204 });
      }
      return new Response('miss', { status: 404 });
    });
    document.cookie = 'cs_csrf=tok';

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<SettingsForm />, { wrapper: withQuery(qc) });

    await userEvent.click(await screen.findByRole('checkbox'));
    await userEvent.clear(screen.getByRole('spinbutton'));
    await userEvent.type(screen.getByRole('spinbutton'), '500');
    await userEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find(([u, i]) =>
        String(u).endsWith('/api/me/settings') && (i as RequestInit | undefined)?.method === 'POST',
      );
      expect(call).toBeTruthy();
      const body = JSON.parse(String((call![1] as RequestInit).body));
      expect(body.transcript_retention_days).toBe(90);
    });
  });
});
```

- [ ] **Step 5: Run + commit**

```bash
npx vitest run
git add web/src/api/hooks.ts web/src/components/SettingsForm.tsx web/src/routes/settings.tsx web/tests/components/SettingsForm.test.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): /settings with transcript retention clamp"
```

---

## Task 13: proto module — binary pty.data encode/decode

**Goal:** TS counterpart of Go's `internal/proto/ptydata.go` so the WS hook can frame/unframe binary frames.

**Files:**
- Create: `web/src/proto/ptydata.ts`
- Create: `web/tests/proto/ptydata.test.ts`

- [ ] **Step 1: Add ulid dependency**

```bash
cd web
npm install ulid
```

- [ ] **Step 2: Write failing test**

`web/tests/proto/ptydata.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { ulid } from 'ulid';
import { encodePTYData, decodePTYData, BINARY_FRAME_VERSION } from '@/proto/ptydata';

describe('pty.data binary frame', () => {
  it('round-trips id + payload', () => {
    const id = ulid();
    const payload = new TextEncoder().encode('hello\x1b[0m');
    const frame = encodePTYData(id, payload);
    expect(frame[0]).toBe(BINARY_FRAME_VERSION);
    const out = decodePTYData(frame);
    expect(out.session).toBe(id);
    expect(new TextDecoder().decode(out.payload)).toBe('hello\x1b[0m');
  });

  it('rejects wrong version', () => {
    const buf = new Uint8Array(17);
    buf[0] = 0x99;
    expect(() => decodePTYData(buf)).toThrow(/version/);
  });

  it('rejects truncated frame', () => {
    expect(() => decodePTYData(new Uint8Array([0x01, 0x02]))).toThrow(/short/);
  });
});
```

- [ ] **Step 3: Run failing**

```bash
npx vitest run tests/proto/ptydata.test.ts
```

- [ ] **Step 4: Implement**

`web/src/proto/ptydata.ts`:

```ts
// Mirror of internal/proto/ptydata.go on the Go side.
//   byte 0     : version (0x01)
//   bytes 1..16: ULID session id (16 bytes, Crockford base32 string in Go;
//                here we represent as the 26-char ULID string both ways).
//   bytes 17..: raw payload.
//
// We accept the wire 16-byte form (Mongo-driver Go packs the ULID as 16
// raw bytes, not the 26-char representation). Convert via the ulid
// library's parse/format helpers.

import { decodeTime, encodeTime, ulid as makeULID, monotonicFactory } from 'ulid';

export const BINARY_FRAME_VERSION = 0x01;

const HEADER_LEN = 17;

export function encodePTYData(sessionULID: string, payload: Uint8Array): Uint8Array {
  if (sessionULID.length !== 26) {
    throw new Error(`encodePTYData: bad ulid len ${sessionULID.length}`);
  }
  const idBytes = ulidToBytes(sessionULID);
  const out = new Uint8Array(HEADER_LEN + payload.length);
  out[0] = BINARY_FRAME_VERSION;
  out.set(idBytes, 1);
  out.set(payload, HEADER_LEN);
  return out;
}

export interface DecodedPTYData {
  session: string;     // ULID string
  payload: Uint8Array;
}

export function decodePTYData(frame: Uint8Array): DecodedPTYData {
  if (frame.length < HEADER_LEN) {
    throw new Error(`decodePTYData: short frame ${frame.length}`);
  }
  if (frame[0] !== BINARY_FRAME_VERSION) {
    throw new Error(`decodePTYData: unsupported version ${frame[0]}`);
  }
  const idBytes = frame.subarray(1, 17);
  const session = bytesToULID(idBytes);
  const payload = frame.slice(HEADER_LEN);
  return { session, payload };
}

// Crockford base32 encode/decode for the 16-byte ULID payload. The `ulid`
// npm package emits 26-char strings from a Date+entropy; we need the
// raw-bytes round-trip so a separate helper is required.

const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

function ulidToBytes(s: string): Uint8Array {
  if (s.length !== 26) throw new Error(`ulidToBytes: bad len`);
  const out = new Uint8Array(16);
  // 26 base32 chars = 130 bits. The ULID spec stores 128 bits with the
  // first char limited to 0-7; we treat the string as a base32 number
  // big-endian and write 16 bytes.
  let bits = 0;
  let acc = 0;
  let outIdx = 0;
  for (let i = 0; i < 26; i++) {
    const v = ALPHABET.indexOf(s[i].toUpperCase());
    if (v < 0) throw new Error(`ulidToBytes: bad char '${s[i]}'`);
    acc = (acc << 5) | v;
    bits += 5;
    while (bits >= 8) {
      bits -= 8;
      out[outIdx++] = (acc >> bits) & 0xff;
      if (outIdx === 16) return out;
    }
  }
  return out;
}

function bytesToULID(b: Uint8Array): string {
  if (b.length !== 16) throw new Error(`bytesToULID: bad len`);
  let acc = 0n;
  for (const byte of b) acc = (acc << 8n) | BigInt(byte);
  let s = '';
  for (let i = 25; i >= 0; i--) {
    s = ALPHABET[Number(acc & 0x1fn)] + s;
    acc >>= 5n;
  }
  return s;
}

// Re-export commonly-used ULID functions so callers don't need a second import.
export const ulid = makeULID;
export { decodeTime, encodeTime, monotonicFactory };
```

- [ ] **Step 5: Run + commit**

```bash
npx vitest run tests/proto/ptydata.test.ts
git add web/src/proto web/tests/proto web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): proto/ptydata.ts encode/decode mirrors Go"
```

---

## Task 14: useSessionStream hook (WS lifecycle)

**Goal:** A hook that opens `/ws/sessions/:id?ct=<csrf>`, decodes incoming `pty.data` binary frames, exposes `write(bytes)` and `resize(cols,rows)`, and reconnects with backoff.

**Files:**
- Create: `web/src/hooks/useSessionStream.ts`
- Create: `web/tests/hooks/useSessionStream.test.tsx`
- Modify: `web/package.json` — add mock-socket.

- [ ] **Step 1: Add deps**

```bash
cd web
npm install -D mock-socket
```

- [ ] **Step 2: Write failing test**

`web/tests/hooks/useSessionStream.test.tsx`:

```tsx
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { Server } from 'mock-socket';
import { useSessionStream } from '@/hooks/useSessionStream';
import { encodePTYData, decodePTYData } from '@/proto/ptydata';

const SID = '01HN0000000000000000000000';

beforeEach(() => {
  document.cookie = 'cs_csrf=tok';
});
afterEach(() => {
  document.cookie = '';
});

describe('useSessionStream', () => {
  it('writes received pty.data into the onData callback', async () => {
    const server = new Server(`ws://localhost/ws/sessions/${SID}?ct=tok`);
    const received: Uint8Array[] = [];
    const onData = (b: Uint8Array) => received.push(b);

    server.on('connection', (sock) => {
      const payload = new TextEncoder().encode('boo');
      const frame = encodePTYData(SID, payload);
      sock.send(frame.buffer);
    });

    renderHook(() =>
      useSessionStream({
        sessionID: SID,
        onData,
        onControl: () => {},
        wsBase: 'ws://localhost',
      }),
    );

    await waitFor(() => expect(received.length).toBe(1));
    expect(new TextDecoder().decode(received[0])).toBe('boo');
    server.stop();
  });

  it('encodes browser write() as a binary frame and sends it', async () => {
    const server = new Server(`ws://localhost/ws/sessions/${SID}?ct=tok`);
    let inbound: Uint8Array | null = null;
    server.on('connection', (sock) => {
      sock.on('message', (m) => {
        if (m instanceof ArrayBuffer) inbound = new Uint8Array(m);
        else if (m instanceof Blob) m.arrayBuffer().then((ab) => (inbound = new Uint8Array(ab)));
      });
    });

    const { result } = renderHook(() =>
      useSessionStream({
        sessionID: SID,
        onData: () => {},
        onControl: () => {},
        wsBase: 'ws://localhost',
      }),
    );
    await act(async () => {
      await new Promise((r) => setTimeout(r, 30));
      result.current.write(new TextEncoder().encode('hi'));
      await new Promise((r) => setTimeout(r, 30));
    });
    expect(inbound).not.toBeNull();
    const dec = decodePTYData(inbound!);
    expect(new TextDecoder().decode(dec.payload)).toBe('hi');
    server.stop();
  });
});
```

- [ ] **Step 3: Implement**

`web/src/hooks/useSessionStream.ts`:

```ts
import { useEffect, useRef, useState } from 'react';
import { encodePTYData, decodePTYData } from '@/proto/ptydata';

export interface ControlFrame {
  v: number;
  type: string;
  session?: string;
  payload?: unknown;
}

export interface SessionStreamOptions {
  sessionID: string;
  onData: (bytes: Uint8Array) => void;
  onControl: (frame: ControlFrame) => void;
  wsBase?: string; // default: derived from window.location
  baseDelayMs?: number;
  maxDelayMs?: number;
}

export interface SessionStreamHandle {
  write(bytes: Uint8Array): void;
  resize(cols: number, rows: number): void;
  status: 'connecting' | 'open' | 'closed';
}

export function useSessionStream(opts: SessionStreamOptions): SessionStreamHandle {
  const [status, setStatus] = useState<SessionStreamHandle['status']>('connecting');
  const ref = useRef<WebSocket | null>(null);
  const optsRef = useRef(opts);
  optsRef.current = opts;

  useEffect(() => {
    let attempts = 0;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    function url(): string {
      const csrf = readCookie('cs_csrf') ?? '';
      const base = opts.wsBase ?? autoBase();
      return `${base}/ws/sessions/${opts.sessionID}?ct=${encodeURIComponent(csrf)}`;
    }

    function connect() {
      if (stopped) return;
      setStatus('connecting');
      const sock = new WebSocket(url());
      sock.binaryType = 'arraybuffer';
      ref.current = sock;

      sock.onopen = () => {
        attempts = 0;
        setStatus('open');
      };
      sock.onmessage = (ev) => {
        if (typeof ev.data === 'string') {
          try {
            const frame = JSON.parse(ev.data) as ControlFrame;
            optsRef.current.onControl(frame);
          } catch { /* ignore malformed */ }
        } else if (ev.data instanceof ArrayBuffer) {
          try {
            const dec = decodePTYData(new Uint8Array(ev.data));
            optsRef.current.onData(dec.payload);
          } catch { /* ignore malformed */ }
        }
      };
      sock.onclose = () => {
        if (stopped) return;
        setStatus('closed');
        const base = opts.baseDelayMs ?? 1000;
        const cap = opts.maxDelayMs ?? 30000;
        const exp = Math.min(cap, base * 2 ** attempts++);
        const jitter = exp * 0.25 * Math.random();
        timer = setTimeout(connect, exp + jitter);
      };
      sock.onerror = () => { sock.close(); };
    }

    connect();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
      ref.current?.close(1000, 'leaving');
    };
  }, [opts.sessionID, opts.wsBase]);

  return {
    status,
    write(bytes) {
      const sock = ref.current;
      if (!sock || sock.readyState !== WebSocket.OPEN) return;
      const frame = encodePTYData(opts.sessionID, bytes);
      sock.send(frame.buffer);
    },
    resize(cols, rows) {
      const sock = ref.current;
      if (!sock || sock.readyState !== WebSocket.OPEN) return;
      sock.send(JSON.stringify({
        v: 1, type: 'pty.resize', session: opts.sessionID, payload: { cols, rows },
      }));
    },
  };
}

function readCookie(name: string): string | undefined {
  const target = `${name}=`;
  for (const c of document.cookie.split(';')) {
    const t = c.trim();
    if (t.startsWith(target)) return decodeURIComponent(t.slice(target.length));
  }
  return undefined;
}

function autoBase(): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}`;
}
```

- [ ] **Step 4: Run + commit**

```bash
npx vitest run tests/hooks/useSessionStream.test.tsx
git add web/src/hooks web/tests/hooks web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): useSessionStream WS hook with backoff + binary framing"
```

---

## Task 15: Terminal component (xterm.js wrapper)

**Goal:** A `<Terminal>` component that owns an xterm.js instance, fits the parent, and exposes `write(bytes)` + `onInput(cb)` + `onResize(cb)`.

**Files:**
- Create: `web/src/components/Terminal.tsx`
- Create: `web/tests/components/Terminal.test.tsx`
- Modify: `web/package.json`

- [ ] **Step 1: Add deps**

```bash
cd web
npm install @xterm/xterm @xterm/addon-fit @xterm/addon-web-links @xterm/addon-search
```

- [ ] **Step 2: Implement Terminal.tsx**

`web/src/components/Terminal.tsx`:

```tsx
import { useEffect, useRef } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { SearchAddon } from '@xterm/addon-search';
import '@xterm/xterm/css/xterm.css';

export interface TerminalHandle {
  write(bytes: Uint8Array | string): void;
}

export interface TerminalProps {
  onInput?: (bytes: Uint8Array) => void;
  onResize?: (cols: number, rows: number) => void;
  apiRef?: React.MutableRefObject<TerminalHandle | null>;
  className?: string;
}

export function Terminal({ onInput, onResize, apiRef, className }: TerminalProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const term = new XTerm({
      convertEol: true,
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Consolas, monospace',
      fontSize: 13,
      theme: {
        background: '#0b0b10',
        foreground: '#e6e6e6',
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.loadAddon(new SearchAddon());

    term.open(containerRef.current);
    fit.fit();

    const enc = new TextEncoder();
    term.onData((str) => onInput?.(enc.encode(str)));
    term.onResize(({ cols, rows }) => onResize?.(cols, rows));

    if (apiRef) {
      apiRef.current = {
        write(b) { term.write(typeof b === 'string' ? b : new Uint8Array(b)); },
      };
    }

    const ro = new ResizeObserver(() => fit.fit());
    ro.observe(containerRef.current);

    return () => {
      ro.disconnect();
      if (apiRef) apiRef.current = null;
      term.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <div ref={containerRef} className={className ?? 'h-full w-full'} />;
}
```

- [ ] **Step 3: Smoke test**

`web/tests/components/Terminal.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { Terminal, type TerminalHandle } from '@/components/Terminal';
import { useRef } from 'react';

describe('<Terminal />', () => {
  it('mounts and exposes write() via ref', () => {
    function Host() {
      const ref = useRef<TerminalHandle | null>(null);
      return (
        <>
          <Terminal apiRef={ref} className="h-40 w-60" />
          <button onClick={() => ref.current?.write('hi')}>w</button>
        </>
      );
    }
    const { container } = render(<Host />);
    expect(container.querySelector('.xterm')).toBeTruthy();
  });
});
```

(jsdom doesn't fully render xterm; we just assert the canvas/DOM mount happens.)

- [ ] **Step 4: Run + commit**

```bash
npx vitest run
git add web/src/components/Terminal.tsx web/tests/components/Terminal.test.tsx web/package.json web/package-lock.json
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): xterm.js Terminal component with fit + addons"
```

---

## Task 16: SessionRoute — wire Terminal + useSessionStream

**Goal:** `/sessions/:id` opens the WS, renders the Terminal, and pipes bytes both ways.

**Files:**
- Create: `web/src/components/SessionView.tsx`
- Modify: `web/src/routes/sessions.$id.tsx`

- [ ] **Step 1: Implement SessionView.tsx**

`web/src/components/SessionView.tsx`:

```tsx
import { useRef } from 'react';
import { Terminal, type TerminalHandle } from './Terminal';
import { useSessionStream } from '@/hooks/useSessionStream';
import { useToast } from '@/components/ui/use-toast';
import { Button } from '@/components/ui/button';
import { useNavigate } from '@tanstack/react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';

export function SessionView({ sessionID }: { sessionID: string }) {
  const apiRef = useRef<TerminalHandle | null>(null);
  const { toast } = useToast();
  const nav = useNavigate();
  const qc = useQueryClient();
  const closeMut = useMutation({
    mutationFn: () => apiClient(`/api/sessions/${sessionID}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sessions'] });
      nav({ to: '/' });
    },
  });

  const stream = useSessionStream({
    sessionID,
    onData: (bytes) => apiRef.current?.write(bytes),
    onControl: (frame) => {
      switch (frame.type) {
        case 'wrapper.offline':
          toast({ title: 'Wrapper offline', variant: 'destructive' });
          break;
        case 'session.exited': {
          const p = frame.payload as { exit_code?: number; reason?: string } | undefined;
          toast({ title: `Session exited (${p?.exit_code ?? '?'})`, description: p?.reason });
          break;
        }
        // replay.start / replay.end ignored: xterm.js handles bytes without ordering hints.
      }
    },
  });

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b bg-background px-4 py-2">
        <span className="text-sm font-medium">{sessionID}</span>
        <span className="text-xs text-muted-foreground">{stream.status}</span>
        <Button size="sm" variant="outline" onClick={() => closeMut.mutate()}>
          Close session
        </Button>
      </header>
      <div className="flex-1 overflow-hidden bg-[#0b0b10]">
        <Terminal
          apiRef={apiRef}
          onInput={(b) => stream.write(b)}
          onResize={(c, r) => stream.resize(c, r)}
        />
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Wire route**

`web/src/routes/sessions.$id.tsx`:

```tsx
component: function SessionRoute() {
  const { id } = Route.useParams();
  return (
    <AppShell sidebar={<Sidebar />}>
      <SessionView sessionID={id} />
    </AppShell>
  );
},
```

Add the imports.

- [ ] **Step 3: Build to confirm**

```bash
cd web
npm run build
```

Expected: build green.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/SessionView.tsx web/src/routes/sessions.$id.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): /sessions/:id wires Terminal + useSessionStream"
```

---

## Task 17: Transcript pane

**Goal:** Optional collapsible pane next to the terminal that lists `jsonl.tail` entries (the user's history if they opted in to keep them).

**Files:**
- Create: `web/src/components/Transcript.tsx`
- Modify: `web/src/api/hooks.ts` — add `useSessionMessages`.
- Modify: `web/src/components/SessionView.tsx` — add toggle + render pane.

- [ ] **Step 1: Add hook**

Append to `web/src/api/hooks.ts`:

```ts
interface MessageJSON { ts: string; entry: string }

export function useSessionMessages(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['session', id, 'messages'],
    queryFn: () => apiClient<MessageJSON[]>(`/api/sessions/${id}/messages`),
    enabled,
    staleTime: 10_000,
  });
}
```

- [ ] **Step 2: Implement Transcript.tsx**

`web/src/components/Transcript.tsx`:

```tsx
import { useSessionMessages } from '@/api/hooks';

export function Transcript({ sessionID, visible }: { sessionID: string; visible: boolean }) {
  const messages = useSessionMessages(sessionID, visible);
  if (!visible) return null;

  return (
    <aside className="w-96 shrink-0 overflow-y-auto border-l bg-background p-3 text-sm">
      <h3 className="mb-2 font-medium">Transcript</h3>
      {messages.isLoading && <div className="text-muted-foreground">Loading…</div>}
      {messages.error && <div className="text-destructive">Failed to load</div>}
      {(messages.data ?? []).length === 0 && !messages.isLoading && (
        <div className="text-muted-foreground">No transcript stored.</div>
      )}
      <ul className="space-y-2">
        {(messages.data ?? []).map((m, i) => (
          <li key={i} className="rounded border bg-muted/40 p-2 font-mono text-xs">
            <div className="mb-1 text-muted-foreground">{m.ts}</div>
            <pre className="whitespace-pre-wrap break-words">{m.entry}</pre>
          </li>
        ))}
      </ul>
    </aside>
  );
}
```

- [ ] **Step 3: Toggle in SessionView**

In `web/src/components/SessionView.tsx`, add:

```tsx
import { useState } from 'react';
import { Transcript } from './Transcript';
```

Replace the body to include a toggle button and the pane:

```tsx
const [showTranscript, setShowTranscript] = useState(false);
// …
return (
  <div className="flex h-full">
    <div className="flex flex-1 flex-col">
      <header className="flex items-center justify-between border-b bg-background px-4 py-2">
        <span className="text-sm font-medium">{sessionID}</span>
        <span className="text-xs text-muted-foreground">{stream.status}</span>
        <div className="flex gap-2">
          <Button size="sm" variant="ghost" onClick={() => setShowTranscript((s) => !s)}>
            {showTranscript ? 'Hide transcript' : 'Show transcript'}
          </Button>
          <Button size="sm" variant="outline" onClick={() => closeMut.mutate()}>
            Close session
          </Button>
        </div>
      </header>
      <div className="flex-1 overflow-hidden bg-[#0b0b10]">
        <Terminal
          apiRef={apiRef}
          onInput={(b) => stream.write(b)}
          onResize={(c, r) => stream.resize(c, r)}
        />
      </div>
    </div>
    <Transcript sessionID={sessionID} visible={showTranscript} />
  </div>
);
```

- [ ] **Step 4: Commit**

```bash
git add web/src/api/hooks.ts web/src/components/Transcript.tsx web/src/components/SessionView.tsx
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): collapsible Transcript pane on session view"
```

---

## Task 18: Mobile sidebar (drawer below md)

**Goal:** Below 768 px, the sidebar collapses into a drawer toggled from the TopBar.

**Files:**
- Modify: `web/src/components/AppShell.tsx`
- Modify: `web/src/components/TopBar.tsx`

- [ ] **Step 1: Add a drawer state to AppShell**

Replace `web/src/components/AppShell.tsx`:

```tsx
import { TopBar } from './TopBar';
import { ReactNode, useState } from 'react';
import { Sheet, SheetContent } from '@/components/ui/sheet';

export function AppShell({
  sidebar,
  children,
}: {
  sidebar?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex h-screen flex-col">
      <TopBar onOpenSidebar={() => setOpen(true)} hasSidebar={!!sidebar} />
      <div className="flex flex-1 overflow-hidden">
        {sidebar && (
          <aside className="hidden w-72 shrink-0 border-r bg-muted/40 md:block">
            {sidebar}
          </aside>
        )}
        <main className="flex-1 overflow-hidden">{children}</main>
      </div>
      {sidebar && (
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetContent side="left" className="w-72 p-0">
            {sidebar}
          </SheetContent>
        </Sheet>
      )}
    </div>
  );
}
```

If `Sheet` isn't installed, add it: `npx shadcn@latest add sheet`.

- [ ] **Step 2: Add a hamburger to TopBar (mobile only)**

Update `web/src/components/TopBar.tsx`'s `props`:

```tsx
export function TopBar({
  onOpenSidebar,
  hasSidebar,
}: {
  onOpenSidebar?: () => void;
  hasSidebar?: boolean;
}) {
  // …
  return (
    <header className="flex h-14 items-center justify-between border-b bg-background px-4">
      <div className="flex items-center gap-2">
        {hasSidebar && (
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-label="Open menu"
            onClick={onOpenSidebar}
          >
            <Menu className="h-5 w-5" />
          </Button>
        )}
        <Link to="/" className="text-lg font-semibold">claude-switch</Link>
      </div>
      {/* …rest unchanged… */}
    </header>
  );
}
```

Add `import { Menu } from 'lucide-react';`.

- [ ] **Step 3: Build + commit**

```bash
cd web
npm run build
git add web/src/components/AppShell.tsx web/src/components/TopBar.tsx web/components.json web/src/components/ui
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(web): mobile drawer for sidebar"
```

---

## Task 19: Update internal/webfs to embed `web/dist`

**Goal:** The Go server stops serving the stub and starts serving the real bundle (when built).

**Files:**
- Modify: `internal/webfs/webfs.go`
- Modify: `internal/webfs/webfs_test.go` (assertion text)

- [ ] **Step 1: Edit webfs.go**

Replace the embed lines and the `Handler()` body in `internal/webfs/webfs.go`:

```go
//go:build !noweb

package webfs

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:../../web/dist
var bundle embed.FS

// Handler returns an http.Handler that serves the embedded bundle. Routes
// without a matching file fall back to index.html so client-side routing works.
func Handler() http.Handler {
	root, err := fs.Sub(bundle, "web/dist")
	if err != nil {
		panic(err)
	}
	fileSrv := http.FileServer(http.FS(root))
	indexBytes, _ := fs.ReadFile(root, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".") && r.URL.Path != "/" {
			fileSrv.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	})
}

// Enabled reports whether the binary embeds a web bundle (true here).
func Enabled() bool { return true }
```

The `stub/` directory and its `index.html` are no longer used. Delete them:

```bash
rm -rf internal/webfs/stub
```

- [ ] **Step 2: Update test**

`internal/webfs/webfs_test.go`:

```go
//go:build !noweb

package webfs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerServesIndex(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	body, _ := io.ReadAll(rr.Body)
	require.True(t, strings.Contains(string(body), "<div id=\"root\">"))
}

func TestHandlerSpaFallback(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/some/spa/route", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}
```

(Now expecting the real `index.html` shipped from the build.)

- [ ] **Step 3: Build the web bundle so the embed has content**

```bash
cd web
npm run build
cd ..
```

- [ ] **Step 4: Run server tests**

```bash
wsl.exe -d Debian -- bash -lc "cd /mnt/c/Proyectos/claude-switch && go test ./internal/webfs/..."
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webfs web/dist
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "feat(webfs): embed web/dist (subsystem 3 bundle)"
```

(Yes, we commit `web/dist/`. It's the artifact the embed reads at compile time. CI will rebuild it and the `git diff` should be small/empty, but committing it lets a fresh `git clone && go build ./cmd/claude-switch-server` work without Node installed. If you'd rather not commit the dist, add it to `.gitignore` and rely on a build step in CI/Dockerfile that runs `npm run build` before `go build`. The plan ships with it committed for simplicity.)

---

## Task 20: CI extension — web build + test + e2e

**Goal:** Two new jobs: `web-tests` runs Vitest + lint; `web-e2e` builds the bundle, boots the Go server with stub OAuth (`-tags e2e`?) and runs Playwright once. The Dockerfile already builds the server image; no change needed there as long as `web/dist/` is committed.

**Files:**
- Modify: `.github/workflows/ci.yml`
- Create: `web/playwright.config.ts`
- Create: `web/tests/e2e/happy-path.spec.ts`

- [ ] **Step 1: Add Playwright**

```bash
cd web
npm install -D @playwright/test
npx playwright install --with-deps chromium
```

- [ ] **Step 2: playwright config**

`web/playwright.config.ts`:

```ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: 'tests/e2e',
  timeout: 30_000,
  use: {
    baseURL: 'http://localhost:5173',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run preview -- --port 5173 --host 127.0.0.1',
    url: 'http://localhost:5173',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
```

- [ ] **Step 3: One happy-path spec**

`web/tests/e2e/happy-path.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test('login screen renders provider buttons', async ({ page }) => {
  await page.route('**/api/me', (r) => r.fulfill({
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify({ providers_configured: ['github', 'google'] }),
  }));

  await page.goto('/login');
  await expect(page.getByRole('link', { name: /github/i })).toBeVisible();
  await expect(page.getByRole('link', { name: /google/i })).toBeVisible();
});
```

(The full pair-and-stream e2e is the server's responsibility — subsystem 2 already has it. Here we just verify the SPA renders correctly against a faked API.)

- [ ] **Step 4: package.json scripts**

Add to `web/package.json` `scripts`:

```json
"test:e2e": "playwright test"
```

- [ ] **Step 5: CI workflow**

Append to `.github/workflows/ci.yml`:

```yaml
  web-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20', cache: 'npm', cache-dependency-path: 'web/package-lock.json' }
      - run: npm ci
        working-directory: web
      - run: npm run lint
        working-directory: web
      - run: npm test
        working-directory: web

  web-e2e:
    runs-on: ubuntu-latest
    needs: web-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20', cache: 'npm', cache-dependency-path: 'web/package-lock.json' }
      - run: npm ci
        working-directory: web
      - run: npx playwright install --with-deps chromium
        working-directory: web
      - run: npm run build
        working-directory: web
      - run: npm run test:e2e
        working-directory: web
```

- [ ] **Step 6: Push + verify**

```bash
git add web/playwright.config.ts web/tests/e2e web/package.json web/package-lock.json .github/workflows/ci.yml
git -c user.email=joorge@gmail.com -c user.name="Jorge Leal" commit -m "ci: web-tests + web-e2e jobs"
git push origin master
gh run watch -R jleal52/claude-switch
```

If the run fails on a system-specific issue, report and stop — don't iterate.

---

## Task 21: Tag v0.3.0

**Goal:** Cut a release with both subsystems' artifacts.

- [ ] **Step 1: Sanity check**

```bash
cd web && npm run build && cd ..
go test ./...
go build ./...
```

Expected: all green.

- [ ] **Step 2: Tag**

```bash
git tag v0.3.0
git push origin v0.3.0
```

- [ ] **Step 3: Watch release workflow**

```bash
gh run list -R jleal52/claude-switch --workflow=release.yml --limit 1
gh run watch -R jleal52/claude-switch
```

Expected: green; the release page lists `claude-switch_*` (wrapper, 5 OS/arch combos) + `claude-switch-server_*` (server, 4 OS/arch combos) + checksums.

The web bundle ships embedded inside `claude-switch-server` binaries — no separate `web` artifact in the release.

---

## Final steps (post-plan execution)

1. `make codegen-ts` and ensure `web/src/api/types.ts` is up to date.
2. `cd web && npm run build` — confirm `web/dist/` is fresh.
3. `go build -tags noweb ./cmd/claude-switch-server` — confirm headless build still works.
4. Run `claude-switch-server` locally with a real Mongo and a real GitHub OAuth app (or stub provider) to verify the full UX end-to-end manually.
5. Update `README.md` with the dev workflow:

```
# Backend dev
docker compose up -d
go run ./cmd/claude-switch-server

# Frontend dev (separate terminal)
cd web
npm install
npm run dev    # opens at http://localhost:5173, proxies /api + /ws to :8080
```

## Notes for the implementer

- **Don't add features the spec doesn't mention.** No PWA, no i18n, no themes beyond light/dark, no settings/profile pages beyond what's in `<SettingsForm>`.
- **Bundle-size budget: ≤ 350 KB gzipped JS initial.** If `vite build`'s "gzip:" line on the largest chunk exceeds 350 KB, stop and report.
- **Keep components focused.** If a `.tsx` file approaches ~250 lines, split before committing.
- **Run `npm run lint && npm test` before each commit.** CI matches that.
- **Commit messages mirror the imperative subjects shown above.** ≤ 70 chars.
