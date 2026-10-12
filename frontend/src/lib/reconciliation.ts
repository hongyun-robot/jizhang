export interface ReconciliationTask {
  key: string;
  repeat: boolean;
  run: () => Promise<unknown>;
}
interface RunningTask {
  started: boolean;
  promise: Promise<unknown>;
}
const running = new Map<string, RunningTask>();
export function startReconciliation(
  tasks: ReconciliationTask[],
  epoch: number,
  currentEpoch: () => number,
  interval = 60_000
): () => void {
  let stopped = false;
  const owned = new Map<string, RunningTask>();
  const invoke = (task: ReconciliationTask) => {
    if (stopped || currentEpoch() !== epoch) return;
    const key = `${epoch}:${task.key}`;
    if (running.has(key)) return;
    const entry: RunningTask = { started: false, promise: Promise.resolve() };
    entry.promise = Promise.resolve()
      .then(() => {
        if (stopped || currentEpoch() !== epoch) return;
        entry.started = true;
        return task.run();
      })
      .catch(() => {})
      .finally(() => {
        if (running.get(key) === entry) running.delete(key);
        if (owned.get(key) === entry) owned.delete(key);
      });
    running.set(key, entry);
    owned.set(key, entry);
  };
  for (const task of tasks) invoke(task);
  const timer = setInterval(() => {
    for (const task of tasks) if (task.repeat) invoke(task);
  }, interval);
  return () => {
    stopped = true;
    clearInterval(timer);
    for (const [key, entry] of owned) {
      if (!entry.started && running.get(key) === entry) running.delete(key);
    }
  };
}
