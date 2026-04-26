// Mirror of internal/proto/ptydata.go on the Go side.
//   byte 0     : version (0x01)
//   bytes 1..16: ULID session id (16 raw bytes; ULID library exposes 26-char
//                Crockford base32 strings, so this module converts both ways).
//   bytes 17..: raw payload.

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

const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

function ulidToBytes(s: string): Uint8Array {
  if (s.length !== 26) throw new Error(`ulidToBytes: bad len`);
  let acc = 0n;
  for (let i = 0; i < 26; i++) {
    const v = ALPHABET.indexOf(s[i].toUpperCase());
    if (v < 0) throw new Error(`ulidToBytes: bad char '${s[i]}'`);
    acc = (acc << 5n) | BigInt(v);
  }
  const out = new Uint8Array(16);
  for (let i = 15; i >= 0; i--) {
    out[i] = Number(acc & 0xffn);
    acc >>= 8n;
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
