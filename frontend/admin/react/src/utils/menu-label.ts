/**
 * 菜单标题 / 同步出的权限名称翻译
 *
 * 菜单 meta.title 和「同步权限」生成的权限点、权限分组名称存的是 routes 命名空间的
 * i18n key（如 menu.system.config）；手工填写的名称是普通文本，原样显示。
 * 操作类权限点的名称形如 menu.permission.menu#edit（页面/模块名#动作），显示成「菜单管理 · 编辑」。
 */
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';

// 形如 menu.system.config 的点分 key；含空格、中文或冒号的文本不当作 key
const I18N_KEY_PATTERN = /^[A-Za-z][\w-]*(\.[\w-]+)+$/;
// 页面/模块名与动作的分隔符，与后端 permissionActionSep 一致
const ACTION_SEP = '#';
const ACTION_PATTERN = /^\w+$/;

type RoutesT = (key: string, options?: { defaultValue?: string }) => string;

/** 动作（view/create/edit/delete…）的显示文字，未登记的动作原样返回 */
export function translatePermissionAction(tRoutes: RoutesT, action: string): string {
  return tRoutes(`permissionAction.${action}`, { defaultValue: action });
}

function translateKey(tRoutes: RoutesT, label: string): string {
  if (!I18N_KEY_PATTERN.test(label)) return label;
  return tRoutes(label, { defaultValue: label });
}

export function translateMenuLabel(tRoutes: RoutesT, label: string | undefined): string {
  if (!label) return '';
  const sep = label.lastIndexOf(ACTION_SEP);
  if (sep > 0) {
    const action = label.slice(sep + 1);
    if (ACTION_PATTERN.test(action)) {
      const owner = translateKey(tRoutes, label.slice(0, sep));
      return `${owner} · ${translatePermissionAction(tRoutes, action)}`;
    }
  }
  return translateKey(tRoutes, label);
}

/** 组件内使用：返回绑定当前语言的翻译函数 */
export function useMenuLabel(): (label: string | undefined) => string {
  const { t: tRoutes } = useTranslation('routes');
  return useCallback((label) => translateMenuLabel(tRoutes, label), [tRoutes]);
}

/** 组件内使用：返回动作翻译函数（如 edit → 编辑） */
export function usePermissionAction(): (action: string) => string {
  const { t: tRoutes } = useTranslation('routes');
  return useCallback((action) => translatePermissionAction(tRoutes, action), [tRoutes]);
}
