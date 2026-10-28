import { useEffect, useState } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { Spin } from 'antd';
import axios from 'axios';
import { useAuth } from '../stores/auth';
import { session } from '../auth/session';
import { isUnauthorized, normalizeError } from '../api/errors';
import QueryState from './QueryState';

export default function RequireAuth({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = useAuth((s) => s.user);
  const booted = useAuth((s) => s.booted);
  const epoch = useAuth((s) => s.epoch);
  const validationPending = useAuth((s) => s.validationPending);
  const validationError = useAuth((s) => s.validationError);
  const location = useLocation();
  const [error, setError] = useState<Error | null>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (booted) return;
    let active = true;
    void session.restore().catch((caught) => {
      if (!active || axios.isCancel(caught)) return;
      if (isUnauthorized(caught)) session.end(normalizeError(caught).message);
      else setError(normalizeError(caught));
    });
    return () => {
      active = false;
    };
  }, [booted, epoch, retry]);
  if (user) return <>
    {validationError && <QueryState error={validationError} onRetry={() => { void session.validate().catch(() => {}); }} />}
    <div style={{ display: 'contents' }} inert={validationPending || !!validationError}>{children}</div>
  </>;
  if (error && !booted)
    return (
      <QueryState
        error={error}
        onRetry={() => {
          setError(null);
          setRetry((value) => value + 1);
        }}
      />
    );
  if (!booted)
    return (
      <div
        style={{ display: 'grid', placeItems: 'center', minHeight: '100dvh' }}
      >
        <Spin size="large" />
      </div>
    );
  return (
    <Navigate
      to="/login"
      state={{ from: location.pathname + location.search + location.hash }}
      replace
    />
  );
}
