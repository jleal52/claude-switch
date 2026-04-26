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
