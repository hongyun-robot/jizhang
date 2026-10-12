import { useEffect, useRef, useState, type TouchEvent } from 'react';

export const PTR_H = 32;
export const PTR_TRIGGER = 56;
export function useListGestures(
  enabled: boolean,
  source: {
    isFetchingNextPage: boolean;
    hasNextPage: boolean;
    isRefetching: boolean;
    isError: boolean;
    refetch: () => unknown;
  },
  loadNext: () => Promise<void>
) {
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const latest = useRef({ source, loadNext });
  latest.current = { source, loadNext };
  const [pull, setPull] = useState(0);
  const start = useRef<number | null>(null);
  useEffect(() => {
    const element = sentinelRef.current;
    if (!enabled || !element) return;
    const observer = new IntersectionObserver(
      (entries) => {
        const state = latest.current;
        if (
          entries.at(-1)?.isIntersecting &&
          window.scrollY > 0 &&
          state.source.hasNextPage &&
          !state.source.isFetchingNextPage
        )
          void state.loadNext();
      },
      { rootMargin: '240px 0px' }
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [enabled]);
  const onTouchStart = (event: TouchEvent) => {
    if (!enabled || window.scrollY > 0 || source.isRefetching || source.isError)
      return;
    start.current = event.touches[0]?.clientY ?? null;
  };
  const onTouchMove = (event: TouchEvent) => {
    if (start.current === null) return;
    const distance =
      (event.touches[0]?.clientY ?? start.current) - start.current;
    setPull(Math.max(0, Math.min(distance * 0.5, PTR_H * 2)));
  };
  const cancel = () => {
    start.current = null;
    setPull(0);
  };
  const onTouchEnd = () => {
    const refresh = start.current !== null && pull >= PTR_TRIGGER;
    cancel();
    if (refresh) void source.refetch();
  };
  return {
    sentinelRef,
    pull,
    onTouchStart,
    onTouchMove,
    onTouchEnd,
    onTouchCancel: cancel,
  };
}
