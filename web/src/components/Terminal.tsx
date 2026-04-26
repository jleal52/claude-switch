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
