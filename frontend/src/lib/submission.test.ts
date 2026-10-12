import { describe, expect, it, vi } from 'vitest';
import { submissionGate } from './submission';
describe('submit validation concurrency', () => {
  it('ignores a second click while validation is pending and releases after failure', async () => {
    const gate = submissionGate();
    let finish!: () => void;
    const run = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        })
    );
    const first = gate(run);
    await gate(run);
    expect(run).toHaveBeenCalledTimes(1);
    finish();
    await first;
    await expect(
      gate(async () => {
        throw new Error('failed');
      })
    ).rejects.toThrow('failed');
    await gate(async () => {});
  });
});
