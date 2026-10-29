import { Navigate } from 'react-router-dom';

import { createLazyRoute, type AppRouteObject } from '@/core/router';
import { AuthGuard } from '@/router/guards';
import MainLayout from '@/layouts/MainLayout';

/**
 * 静态基础路由配置
 */
export const staticRoutes: AppRouteObject[] = [
  // 主布局容器（所有需要登录的页面都在这里）
  // 直接使用 '/' 作为路径，业务路由会作为其子路由
  {
    path: '/',
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    meta: {
      hideInMenu: true,
      hideInTab: true,
    },
    children: [
      // 根路径重定向到分析页（登录落地页，与 ele/vben 的 DEFAULT_HOME_PATH=/analytics 对齐）
      {
        path: '/',
        index: true,
        element: <Navigate to="/dashboard/analytics" replace />,
        meta: { title: 'routes:home', hideInMenu: true, hideInTab: true },
      },
    ],
  },
];

// 后端模式固定注册的系统页：独立于菜单授权，仍由 AuthGuard 保护。
export const backendSystemRoutes: AppRouteObject[] = [{
  element: <AuthGuard><MainLayout /></AuthGuard>,
  meta: { hideInMenu: true, hideInTab: true },
  children: [
    {
      path: '/opm/profile',
      element: createLazyRoute(() => import('@/pages/app/opm/user/profile')),
      meta: { title: 'routes:user-profile', hideInMenu: true },
    },
    {
      path: '/internal-message/inbox',
      element: createLazyRoute(() => import('@/pages/app/internal-message/inbox')),
      meta: { title: 'routes:inbox', hideInMenu: true },
    },
  ],
}];
