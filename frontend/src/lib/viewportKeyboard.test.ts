import { describe, expect, it, vi } from 'vitest';
import { createKeyboardObserver } from './viewportKeyboard';

describe('shared keyboard inset', () => {
  it('keeps the inset until the last popup closes and cleans observers once', () => {
    const write = vi.fn();
    const viewport = {
      height: 500,
      offsetTop: 0,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    };
    const observer = createKeyboardObserver({
      height: () => 800,
      viewport,
      write,
      frame: () => 1,
      cancel: vi.fn(),
    });
    const first = observer.acquire(),
      second = observer.acquire();
    expect(viewport.addEventListener).toHaveBeenCalledTimes(2);
    expect(write).toHaveBeenLastCalledWith(300);
    first();
    expect(write).toHaveBeenLastCalledWith(300);
    second();
    expect(write).toHaveBeenLastCalledWith(null);
    expect(viewport.removeEventListener).toHaveBeenCalledTimes(2);
    second();
    expect(write).toHaveBeenCalledTimes(2);
  });
});
