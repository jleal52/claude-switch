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
