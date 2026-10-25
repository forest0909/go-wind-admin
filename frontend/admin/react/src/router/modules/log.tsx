import type { AppRouteObject } from '@/core/router/types';
import { createLazyRoute } from '@/core/router';

/**
 * 日志审计管理路由配置
 * 包括登录审计、操作审计、Redis 缓存监控
 */
export const logRoutes: AppRouteObject[] = [
  {
    name: 'log',
    path: 'log', // 相对路径，会自动拼接到父路由 '/'
    meta: {
      title: 'routes:log',
      icon: 'lucide:logs', // Iconify 格式
      order: 2004,
      keepAlive: true, // 保持组件状态
      // permission: 'sys:platform_admin', // 平台管理员权限（开发阶段暂时注释）
    },
    children: [
      {
        name: 'login-audit-log',
        path: 'login-audit-logs', // 相对路径，最终为 /log/login-audit-logs
        element: createLazyRoute(() => import('@/pages/app/log/login-audit-log')),
        meta: {
          title: 'routes:login-audit-log',
          icon: 'lucide:user-lock',
          order: 1,
          // permission: 'sys:platform_admin', // 平台管理员权限（开发阶段暂时注释）
        },
      },
      {
        name: 'operation-audit-log',
        path: 'operation-audit-logs', // 相对路径，最终为 /log/operation-audit-logs
        element: createLazyRoute(() => import('@/pages/app/log/operation-audit-log')),
        meta: {
          title: 'routes:operation-audit-log',
          icon: 'lucide:shield-ellipsis', // Iconify 格式
          order: 3,
          // permission: 'sys:platform_admin', // 平台管理员权限（开发阶段暂时注释）
        },
      },
    ],
  },
];

export default logRoutes;
