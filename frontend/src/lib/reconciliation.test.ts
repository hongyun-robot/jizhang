import { describe, expect, it, vi } from 'vitest';
import { startReconciliation } from './reconciliation';

describe('background reconciliation lifecycle', () => {
  it('starts the live remount when StrictMode cleaned up the queued first mount', async () => {
    const run = vi.fn(async () => {});
    const task = { key: 'strict-remount', repeat: false, run };
    const first = startReconciliation([task], 45, () => 45);
    first();
    const second = startReconciliation([task], 45, () => 45);
    await Promise.resolve();
    await Promise.resolve();
    expect(run).toHaveBeenCalledTimes(1);
    second();
  });
  it('does not start queued work after immediate logout or unmount', async () => {
    const run = vi.fn(async () => {});
    const stop = startReconciliation(
      [{ key: 'queued', repeat: false, run }],
      44,
      () => 44
    );
    stop();
    await Promise.resolve();
    expect(run).not.toHaveBeenCalled();
  });
  it('deduplicates remounts and overlapping ticks and stops old session work', async () => {
    vi.useFakeTimers();
    let done!: () => void;
    let epoch = 42;
    const run = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          done = resolve;
        })
    );
    const first = startReconciliation(
      [{ key: 'money', repeat: true, run }],
      epoch,
      () => epoch
    );
    const second = startReconciliation(
      [{ key: 'money', repeat: true, run }],
      epoch,
      () => epoch
    );
    await vi.advanceTimersByTimeAsync(180_000);
    expect(run).toHaveBeenCalledTimes(1);
    done();
    await vi.advanceTimersByTimeAsync(1);
    epoch++;
    await vi.advanceTimersByTimeAsync(60_000);
    expect(run).toHaveBeenCalledTimes(1);
    first();
    second();
    vi.useRealTimers();
  });
  it('runs startup-only work once and cancels timers when stopped', async () => {
    vi.useFakeTimers();
    const run = vi.fn(async () => {});
    const stop = startReconciliation(
      [{ key: 'startup', repeat: false, run }],
      43,
      () => 43
    );
    await vi.advanceTimersByTimeAsync(120_000);
    stop();
    expect(run).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
    vi.useRealTimers();
  });
});
