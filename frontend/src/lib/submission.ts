/** Protects the entire validation/write attempt, before a mutation is pending. */
export function submissionGate() {
  let busy = false;
  return async (run: () => Promise<void>): Promise<void> => {
    if (busy) return;
    busy = true;
    try {
      await run();
    } finally {
      busy = false;
    }
  };
}
