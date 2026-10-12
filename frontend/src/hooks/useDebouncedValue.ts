import { useEffect, useState } from 'react';

export function useDebouncedValue<T>(
  value: T,
  immediate: boolean,
  delay = 250
): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    if (immediate) {
      setSettled(value);
      return;
    }
    const timer = window.setTimeout(() => setSettled(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, immediate, delay]);
  return immediate ? value : settled;
}
