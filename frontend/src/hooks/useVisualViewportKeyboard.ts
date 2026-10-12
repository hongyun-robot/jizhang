import { useEffect } from 'react';
import { createKeyboardObserver } from '../lib/viewportKeyboard';
let observer: ReturnType<typeof createKeyboardObserver> | undefined;
export function useVisualViewportKeyboard(active = true): void {
  useEffect(() => {
    if (!active) return;
    observer ??= createKeyboardObserver({
      height: () => window.innerHeight,
      viewport: window.visualViewport,
      write: (value) => {
        const root = document.documentElement;
        if (value === null) root.style.removeProperty('--kb-inset');
        else root.style.setProperty('--kb-inset', `${value}px`);
        if (value && value > 0) root.dataset.keyboardOpen = 'true';
        else delete root.dataset.keyboardOpen;
      },
      frame: (callback) => requestAnimationFrame(callback),
      cancel: (id) => cancelAnimationFrame(id),
    });
    return observer.acquire();
  }, [active]);
}
