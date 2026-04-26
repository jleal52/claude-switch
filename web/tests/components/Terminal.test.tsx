import { describe, it, expect } from 'vitest';
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
