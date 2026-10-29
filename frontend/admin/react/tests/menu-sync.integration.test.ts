import { expect, it } from 'vitest';
import { buildSyncMenusRequest } from '../src/api/hooks/menu';
import { businessRoutes } from '../src/router/business-routes';
import { normalizeViewPath } from '../src/core/router/generators/generate-routes-backend';
import type { permissionservicev1_Menu as Menu } from '../src/api/generated/admin/service/v1';

const modules = import.meta.glob<{ default?: unknown }>('../src/pages/app/**/*.tsx', {
  eager: true,
});

it('maps the real MERGE sync payload into the backend pageMap', async () => {
  const pageMap = new Set(
    Object.entries(modules)
      .filter(([, mod]) => typeof mod.default === 'function')
      .map(([key]) => normalizeViewPath(key)),
  );
  const request = await buildSyncMenusRequest(businessRoutes, (key) => key);
  expect(request.mode).toBe('MERGE');
  let pages = 0;
  const hidden: string[] = [];
  const visit = (items: Menu[], parent = '') => {
    for (const menu of items) {
      const path = `${parent}/${menu.path}`.replace(/\/+/g, '/');
      if (menu.meta?.hideInMenu) hidden.push(path);
      if (menu.type === 'MENU') {
        pages++;
        expect(menu.component, menu.path).toBeTruthy();
        expect(pageMap.has(normalizeViewPath(menu.component ?? '')), menu.component).toBe(true);
      }
      visit(menu.children ?? [], path);
    }
  };
  visit(request.items ?? []);
  expect(pages).toBe(24);
  expect(hidden.sort()).toEqual([
    '/internal-message/inbox',
    '/opm/profile',
    '/opm/users/detail/:id',
    '/permission/codes',
    '/tenant',
    '/tenant/members',
    '/tenant/plans',
  ]);
});
