import '@testing-library/jest-dom/vitest';

// Polyfill ResizeObserver for jsdom (xterm.js's FitAddon needs it).
class ResizeObserver { observe() {} unobserve() {} disconnect() {} }
// @ts-ignore
globalThis.ResizeObserver ??= ResizeObserver;

// Polyfill matchMedia for jsdom (xterm.js's CoreBrowserService needs it).
if (!window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (_query: string) => ({
      matches: false,
      media: _query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}
