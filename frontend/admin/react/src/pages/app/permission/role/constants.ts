/**
 * 角色模块枚举映射常量
 */

type TFn = (key: string, options?: Record<string, any>) => string;

// ========== 角色状态（与菜单模块一致：ON/OFF） ==========

export const STATUS_COLORS: Record<string, string> = {
  ON: 'success',
  OFF: 'error',
};

export function getStatusMap(t: TFn) {
  return {
    ON: { text: t('statusMap.ON'), color: STATUS_COLORS.ON },
    OFF: { text: t('statusMap.OFF'), color: STATUS_COLORS.OFF },
  };
}

export function getStatusOptions(t: TFn) {
  return [
    { label: t('statusMap.ON'), value: 'ON' },
    { label: t('statusMap.OFF'), value: 'OFF' },
  ];
}

// ========== 权限树构建 ==========

interface TreeNode {
  key: number | string;
  title: string;
  children?: TreeNode[];
  /** 页面节点：其「查看」权限点的 ID（勾了任一操作时提交时一并带上） */
  viewKey?: number;
}

type PermissionItem = {
  id?: number | string;
  title?: string;
  name?: string;
  code?: string;
  groupId?: number | string;
};

// 操作在页面下的排列顺序，未列出的排在后面
const ACTION_ORDER = ['view', 'create', 'edit', 'delete'];

/** 把 menu:edit 拆成资源 menu 和动作 edit（按最后一个冒号） */
function splitCode(code?: string): [string, string] | null {
  if (!code) return null;
  const i = code.lastIndexOf(':');
  if (i <= 0 || i === code.length - 1) return null;
  return [code.slice(0, i), code.slice(i + 1)];
}

/**
 * 把同一组内的权限点排成「页面 → 操作」：
 * 资源有 <资源>:view 页面权限点、又有其他动作时，生成页面节点，
 * 下挂「查看」和各操作；其余权限点平铺。
 */
function nestByPage(
  perms: PermissionItem[],
  translate: (label: string) => string,
  actionLabel: (action: string) => string,
): TreeNode[] {
  const byResource = new Map<string, PermissionItem[]>();
  for (const p of perms) {
    const parts = splitCode(p.code);
    // 目录权限点（xxx:dir）不是页面上的操作，资源名撞上也不并进页面
    if (!parts || parts[1] === 'dir') continue;
    const list = byResource.get(parts[0]) ?? [];
    list.push(p);
    byResource.set(parts[0], list);
  }

  const label = (p: PermissionItem) => translate(p.title || p.name || p.code || String(p.id));
  const nodes: TreeNode[] = [];
  const done = new Set<PermissionItem>();
  for (const p of perms) {
    if (done.has(p)) continue;
    const parts = splitCode(p.code);
    const siblings = parts && parts[1] !== 'dir' ? (byResource.get(parts[0]) ?? []) : [];
    const page = siblings.find((s) => splitCode(s.code)?.[1] === 'view');
    if (!page || siblings.length < 2) {
      done.add(p);
      nodes.push({ key: Number(p.id), title: label(p) });
      continue;
    }

    siblings.forEach((s) => done.add(s));
    const rank = (s: PermissionItem) => {
      const i = ACTION_ORDER.indexOf(splitCode(s.code)?.[1] ?? '');
      return i < 0 ? ACTION_ORDER.length : i;
    };
    const children = [...siblings]
      .sort((x, y) => rank(x) - rank(y))
      .map((s) => {
        const action = splitCode(s.code)?.[1] ?? '';
        // 同步生成的名称（页面或「名称#动作」）只显示动作；手工起的名称原样显示
        const auto = s === page || (s.name ?? '').includes('#');
        return { key: Number(s.id), title: auto ? actionLabel(action) : label(s) };
      });
    nodes.push({ key: `p_${page.id}`, title: label(page), children, viewKey: Number(page.id) });
  }
  return nodes;
}

/**
 * 根据权限组和权限列表构建权限树。
 * 权限组 API 返回嵌套结构（children 含子组），需递归处理。
 * 每个组节点下附加匹配的权限（通过 groupId 关联），并按「页面 → 操作」分层。
 */
export function buildPermissionTree(
  groups: Array<{ id?: number | string; title?: string; name?: string; code?: string; children?: any[] }>,
  permissions: PermissionItem[],
  {
    translate = (label) => label,
    actionLabel = (action) => action,
  }: {
    /** 名称翻译（i18n key / 「名称#动作」） */
    translate?: (label: string) => string;
    /** 动作翻译（edit → 编辑） */
    actionLabel?: (action: string) => string;
  } = {},
): TreeNode[] {
  if (!groups || groups.length === 0) return [];

  return groups.map((group) => {
    // 子权限组（递归）
    const subGroups = buildPermissionTree(group.children || [], permissions, { translate, actionLabel });
    // 匹配的权限
    const matchedPerms = nestByPage(
      (permissions || []).filter((p) => String(p.groupId) === String(group.id)),
      translate,
      actionLabel,
    );

    const children = [...subGroups, ...matchedPerms];
    return {
      key: `g_${group.id}`,
      title: translate(group.title || group.name || group.code || String(group.id)),
      children: children.length > 0 ? children : undefined,
    };
  });
}

/**
 * 勾了页面下任一操作时补上该页面的「查看」权限点：没有查看就进不了页面，单独的操作没有意义。
 */
export function withPageViews(ids: number[], treeData: TreeNode[]): number[] {
  const result = new Set(ids);
  const walk = (nodes: TreeNode[]) => {
    for (const node of nodes) {
      if (node.viewKey !== undefined && node.children?.some((c) => result.has(c.key as number))) {
        result.add(node.viewKey);
      }
      if (node.children) walk(node.children);
    }
  };
  walk(treeData);
  return [...result];
}

/**
 * 从权限树勾选值中提取所有数字 ID（过滤掉非数字值）
 */
export function filterNumbers(values: any[]): number[] {
  if (!Array.isArray(values)) return [];
  return values
    .flat(Infinity)
    .filter((v) => typeof v === 'number' && !isNaN(v))
    .map((v) => Number(v));
}

/**
 * 递归收集树中所有「叶子节点」（无 children）的 key。
 * 用于提交勾选值时剥离父节点 key：
 * 这些 Tree 默认开启父子联动，勾选某父节点下全部子节点时父节点会被自动勾选，
 * 其 key（权限组/父菜单的 ID）会混入 checkedKeys。若直接 filterNumbers 提交，
 * 组/父菜单 ID 会被后端当作权限/菜单 ID 处理，可能造成越权绑定。
 * 这里用叶子集合对 checkedKeys 求交集，只保留真正的叶子 ID。
 */
export function extractLeafIds(checkedKeys: any[], treeData: any[]): number[] {
  if (!Array.isArray(checkedKeys) || !Array.isArray(treeData)) return [];
  const leafIds = new Set<string | number>();
  const collect = (nodes: any[]) => {
    for (const node of nodes) {
      if (node.children?.length > 0) {
        collect(node.children);
      } else {
        leafIds.add(node.key);
      }
    }
  };
  collect(treeData);
  // 只返回数字 key（权限 ID），过滤字符串 key（权限组 g_xxx）
  return checkedKeys
    .flat(Infinity)
    .filter((v) => leafIds.has(v) && typeof v === 'number')
    .map((v) => Number(v));
}

// ========== 数据权限范围（角色级，五档） ==========

// Tag 颜色语义与 vue-vben/vue-element 既有映射一致：
// ALL=red（全量，最高风险）/ UNIT_AND_CHILD=blue / UNIT_ONLY=orange /
// SELECTED_UNITS=purple / SELF=default。
export const DATA_SCOPE_COLORS: Record<string, string> = {
  ALL: 'red',
  UNIT_AND_CHILD: 'blue',
  UNIT_ONLY: 'orange',
  SELECTED_UNITS: 'purple',
  SELF: 'default',
};

export function getDataScopeMap(t: TFn) {
  const map: Record<string, { text: string; color: string }> = {};
  for (const k of Object.keys(DATA_SCOPE_COLORS)) {
    map[k] = { text: t(`dataScopeMap.${k}`), color: DATA_SCOPE_COLORS[k] };
  }
  return map;
}

export function getDataScopeOptions(t: TFn) {
  return Object.keys(DATA_SCOPE_COLORS).map((k) => ({
    label: t(`dataScopeMap.${k}`),
    value: k,
  }));
}

// ========== 字段权限（资源 × 可勾选字段） ==========

// 可勾选字段：value 与后端 proto 字段 json_name 逐字一致，
// 提交后经登录聚合写入令牌，命中字段在响应侧被裁剪。
// 新增受控资源：在此加 getXxxFieldPermissionOptions + ROLE_FIELD_RESOURCES 登记项，
// 并在服务端加同型装饰器（role_field_permission.go 等）。

/** 受控资源登记：抽屉按此渲染分组，顺序即展示顺序。 */
export const ROLE_FIELD_RESOURCES = [
  { resource: 'User', options: (t: TFn) => getUserFieldPermissionOptions(t) },
  {
    resource: 'Role',
    options: (t: TFn) => [{ value: 'permissions', label: t('fieldPerm.field.permissions') }],
  },
] as const;

export function getUserFieldPermissionOptions(t: TFn) {
  return [
    { value: 'email', label: t('fieldPerm.field.email') },
    { value: 'mobile', label: t('fieldPerm.field.mobile') },
    { value: 'telephone', label: t('fieldPerm.field.telephone') },
    { value: 'address', label: t('fieldPerm.field.address') },
    { value: 'region', label: t('fieldPerm.field.region') },
    { value: 'lastLoginAt', label: t('fieldPerm.field.lastLoginAt') },
    { value: 'lastLoginIp', label: t('fieldPerm.field.lastLoginIp') },
  ];
}

// ========== 组织单元树（SELECTED_UNITS 自定义授权集） ==========

interface OrgTreeNode {
  key: number | string;
  title: string;
  children?: OrgTreeNode[];
}

/**
 * 把组织单元 API 的嵌套树映射为 antd Tree 数据（key/title）。
 * 空 children 一律省略（children:[] 会渲染无效展开箭头）。
 */
export function buildOrgUnitTree(items: any[]): OrgTreeNode[] {
  const result: OrgTreeNode[] = [];
  for (const item of items || []) {
    const node: OrgTreeNode = {
      key: Number(item.id),
      title: item.name || String(item.id),
    };
    const children = buildOrgUnitTree(item.children || []);
    if (children.length > 0) {
      node.children = children;
    }
    result.push(node);
  }
  return result;
}
