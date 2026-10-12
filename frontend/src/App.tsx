import { lazy } from 'react';
import RouteContent from './components/RouteContent';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';

import { BASENAME } from './base';
import AppShell from './components/AppShell';
import RequireAuth from './components/RequireAuth';
import RequireRole from './components/RequireRole';
import { RECORDABLE_ROLES } from './stores/auth';
const Login = lazy(() => import('./pages/Login'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const Records = lazy(() => import('./pages/Records'));
const Stats = lazy(() => import('./pages/Stats'));
const Mine = lazy(() => import('./pages/Mine'));
const Accounts = lazy(() => import('./pages/Accounts'));
const Liabilities = lazy(() => import('./pages/Liabilities'));
const LiabilityDetail = lazy(() => import('./pages/LiabilityDetail'));
const Categories = lazy(() => import('./pages/Categories'));
const Settings = lazy(() => import('./pages/Settings'));
const Audit = lazy(() => import('./pages/Audit'));
const Budgets = lazy(() => import('./pages/Budgets'));
const Recurring = lazy(() => import('./pages/Recurring'));
const Notifications = lazy(() => import('./pages/Notifications'));
const Vouchers = lazy(() => import('./pages/Vouchers'));

export default function App() {
  return (
    <BrowserRouter basename={BASENAME}>
      <RouteContent>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route
            element={
              <RequireAuth>
                <AppShell />
              </RequireAuth>
            }
          >
            <Route path="/" element={<Dashboard />} />
            <Route path="/records" element={<Records />} />
            <Route path="/stats" element={<Stats />} />
            <Route path="/mine" element={<Mine />} />
            <Route path="/vouchers" element={<Vouchers />} />
            <Route path="/liabilities" element={<Liabilities />} />
            <Route path="/liabilities/:id" element={<LiabilityDetail />} />
            {/* 预算：查看对四档角色开放，配置仅管理员（后端 + 页面内双重门禁，specs/budgets） */}
            <Route path="/budgets" element={<Budgets />} />
            {/* 提醒列表：四档角色可读可标已读（specs/notifications，任务 10.3） */}
            <Route path="/notifications" element={<Notifications />} />
            {/* 管理页：入口仅管理员可见，路由级再拦一道（3.4 显隐要求） */}
            <Route
              path="/accounts"
              element={
                <RequireRole role="admin">
                  <Accounts />
                </RequireRole>
              }
            />
            <Route
              path="/categories"
              element={
                <RequireRole role="admin">
                  <Categories />
                </RequireRole>
              }
            />
            <Route
              path="/settings"
              element={
                <RequireRole role="admin">
                  <Settings />
                </RequireRole>
              }
            />
            <Route
              path="/audit"
              element={
                <RequireRole role="admin">
                  <Audit />
                </RequireRole>
              }
            />
            {/* 周期记账：可记账角色自助管理自己的模板（specs/recurring-entries 按创建者归属隔离） */}
            <Route
              path="/recurring"
              element={
                <RequireRole roles={RECORDABLE_ROLES}>
                  <Recurring />
                </RequireRole>
              }
            />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </RouteContent>
    </BrowserRouter>
  );
}
