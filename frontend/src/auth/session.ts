import axios from 'axios';
import { API_BASE } from '../base';
import { useAuth } from '../stores/auth';
import { queryClient } from '../api/queryClient';
import { unwrapBody, type ApiBody } from '../api/errors';
import { clearNavigationMemory } from '../navigation/memory';
import { createSession, type TokenResult } from './coordinator';
import type { SessionUser } from '../domain/identity';

const authApi = axios.create({
  baseURL: API_BASE,
  timeout: 10000,
  withCredentials: true,
});
export const session = createSession({
  read: useAuth.getState,
  write: (token, user) => {
    if (user) useAuth.getState().setSession(token, user);
    else useAuth.getState().setToken(token);
  },
  end: () => {
    useAuth.getState().clear();
    void queryClient.cancelQueries();
    queryClient.clear();
    clearNavigationMemory();
  },
  refresh: async () =>
    unwrapBody(
      (await authApi.post<ApiBody<TokenResult>>('/auth/refresh')).data
    ),
  me: async (token) =>
    unwrapBody(
      (
        await authApi.get<ApiBody<SessionUser>>('/auth/me', {
          headers: { Authorization: `Bearer ${token}` },
        })
      ).data
    ),
  login: async (input) =>
    unwrapBody(
      (await authApi.post<ApiBody<TokenResult>>('/auth/login', input)).data
    ),
  logout: async () => {
    await authApi.post('/auth/logout');
  },
});
