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
