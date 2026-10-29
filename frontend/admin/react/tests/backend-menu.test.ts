import { readFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { cwd } from 'node:process';
import { describe, expect, it, vi } from 'vitest';
import {
  generateRoutesByBackend,
  normalizeViewPath,
} from '../src/core/router/generators/generate-routes-backend';
import { transformRoutesToMenu } from '../src/core/router/utils/menu';
import { createAccessibleRouter } from '../src/core/router/factory';
import { usePreferencesStore } from '../src/core/preferences/store';
import type { BackendRoute } from '../src/core/router/types';

// 路由生成器仅依赖树工具，避免 barrel 引入无关组件的副作用。
vi.mock('@/utils', () => import('../src/utils/tree'));

const seed = readFileSync(resolve(cwd(), '../../../backend/pkg/constants/default_data.go'), 'utf8');
const menus = [
  ...seed.slice(seed.indexOf('var DefaultMenus')).matchAll(/\n\t\{\n\t\tId:([\s\S]*?)\n\t\},/g),
].map((match) => {
  const block = match[1];
  const field = (key: string) =>
    block.match(new RegExp(`${key}:\\s+trans.Ptr\\("([^\"]*)"\\)`))?.[1];
  return {
    type: block.match(/Type:\s+permissionV1.Menu_(\w+)/)?.[1],
    name: field('Name'),
    component: field('Component'),
  };
});

describe('backend menus', () => {
  it('maps every default MENU component to an existing React page', () => {
    const pages = menus.filter((menu) => menu.type === 'MENU');
    expect(pages).toHaveLength(24);
    for (const menu of pages) {
      const path = normalizeViewPath(menu.component ?? '');
      expect(
        existsSync(resolve(cwd(), `src/pages/app${path}.tsx`)) ||
          existsSync(resolve(cwd(), `src/pages/app${path}/index.tsx`)),
        menu.name,
      ).toBe(true);
    }
  });

  it('maps all page imports used by menu sync to the same pageMap keys', () => {
    const modules = [
      'dashboard',
      'opm',
      'tenant',
      'permissions',
      'internal-message',
      'log',
      'system',
    ];
    const imports: string[] = [];
    for (const name of modules) {
      const source = readFileSync(resolve(cwd(), `src/router/modules/${name}.tsx`), 'utf8');
      for (const match of source.matchAll(/import\('(@\/pages\/app\/[^']+)'\)/g)) {
        imports.push(match[1]);
      }
    }
    expect(imports).toHaveLength(24);
    for (const page of imports) {
      const component = page.replace('@/pages/', '') + '/index.vue';
      expect(
        existsSync(resolve(cwd(), `src/pages/app${normalizeViewPath(component)}.tsx`)),
        component,
      ).toBe(true);
    }
  });

  it('retains role menus despite legacy authority and respects hidden metadata', () => {
    const routes = [
      { path: '/granted', meta: { title: 'Renamed', order: 2, authority: ['legacy'] } },
      { path: '/hidden', meta: { hideInMenu: true } },
    ];
    expect(
      transformRoutesToMenu(routes, [], { checkAuthority: false })?.map((menu) => menu.name),
    ).toEqual(['Renamed']);
    expect(transformRoutesToMenu(routes, [])).toEqual([]);
  });

  it('does not render the injected directory redirect route as a blank menu item', () => {
    const routes = [
      {
        path: '/dashboard',
        meta: { title: 'Dashboard' },
        children: [{ index: true }, { path: 'analytics', meta: { title: 'Analytics' } }],
      },
    ];
    const [dashboard] = transformRoutesToMenu(routes, [], { checkAuthority: false }) ?? [];
    expect(dashboard.children?.map((menu) => menu.name)).toEqual(['Analytics']);
  });

  it('keeps only returned pages and redirects directories into the authorized subtree', async () => {
    const menus: BackendRoute[] = [
      {
        path: '/root',
        name: 'root',
        component: 'BasicLayout',
        redirect: '/root/sibling',
        children: [{ path: 'only', name: 'only', component: 'app/opm/user/list/index.vue' }],
      },
    ];
    const Page = () => null;
    const routes = await generateRoutesByBackend({
      fetchMenuListAsync: async () => menus,
      pageMap: { '/opm/user/list': Page },
      layoutMap: { BasicLayout: Page },
    });
    expect(routes[0].redirect).toBe('/root/only');
    expect(routes[0].children?.find((r) => r.path === 'only')?.element).toBeTruthy();
    expect(routes[0].children?.some((r) => r.path === 'sibling')).toBe(false);
  });

  it('keeps static routes with an empty or failed navigation without frontend fallback', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    for (const fail of [false, true]) {
      const bundle = await createAccessibleRouter('backend', {
        routes: [
          { path: '/', children: [{ path: 'forbidden-business-page' }] },
          { path: '/auth/login', meta: { hideInMenu: true } },
          { path: '/error/403', meta: { hideInMenu: true } },
          { path: '*', meta: { hideInMenu: true } },
        ],
        backendStaticRoutes: [
          {
            meta: { hideInMenu: true },
            children: [{ path: '/opm/profile' }, { path: '/internal-message/inbox' }],
          },
        ],
        fetchMenuListAsync: async () => {
          if (fail) throw new Error('offline');
          return [];
        },
      });
      expect(bundle.routes.some((r) => r.path === '/auth/login')).toBe(true);
      expect(
        bundle.routes.some((r) => r.children?.some((c) => c.path === '/internal-message/inbox')),
      ).toBe(true);
      expect(transformRoutesToMenu(bundle.routes, [], { checkAuthority: false })).toEqual([]);
      bundle.router.dispose();
    }
    expect(log).toHaveBeenCalledWith('Failed to generate routes from backend:', expect.any(Error));
    log.mockRestore();
  });

  it('migrates only accessMode from persisted version 2 and retains other preferences', async () => {
    const before = structuredClone(usePreferencesStore.getState().preferences);
    before.app.accessMode = 'frontend';
    before.theme.radius = '12';
    localStorage.setItem(
      'app-preferences',
      JSON.stringify({ state: { preferences: before }, version: 2 }),
    );
    await usePreferencesStore.persist.rehydrate();
    expect(usePreferencesStore.getState().preferences).toEqual({
      ...before,
      app: { ...before.app, accessMode: 'backend' },
    });
    expect(JSON.parse(localStorage.getItem('app-preferences') ?? '{}').version).toBe(3);
  });
});
