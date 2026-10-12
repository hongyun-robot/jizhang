import { useEffect, useRef } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

export function useRecordLocator(ready: boolean, shapeKnown: boolean) {
  const location = useLocation();
  const navigate = useNavigate();
  const locateId = (location.state as { locateId?: number } | null)?.locateId;
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const highlighted = useRef<Element | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
      highlighted.current?.classList.remove('flash');
    },
    []
  );
  useEffect(() => {
    if (!ready || !shapeKnown || !locateId) return;
    const element = document.querySelector(`[data-txn-id="${locateId}"]`);
    if (element) {
      if (timer.current) clearTimeout(timer.current);
      highlighted.current?.classList.remove('flash');
      highlighted.current = element;
      element.scrollIntoView({ block: 'center' });
      element.classList.add('flash');
      timer.current = setTimeout(() => element.classList.remove('flash'), 1800);
    }
    navigate(
      { pathname: '/records', search: location.search, hash: location.hash },
      { replace: true, state: null }
    );
  }, [ready, shapeKnown, locateId, navigate, location.search, location.hash]);
}
