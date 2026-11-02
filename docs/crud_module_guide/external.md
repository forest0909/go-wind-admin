# 外部数据模块

> 后端部分（配置、底层包、Demo 后端）由后端任务补齐，本分支只写了「前端」小节。

## 前端

外部数据页面就是一个普通 CRUD 页面：文件、hooks、ListTable、TableActions、抽屉、关闭守卫、权限按钮、中英文翻译都照 [react.md](react.md) 写。范例是「外部数据示例」联系人页：

| 文件 | 作用 |
|---|---|
| `src/api/hooks/external-demo-contact.ts` | 6 个 hooks，queryKey 前缀 `listExternalDemoContacts` |
| `src/pages/app/system/external-demo-contact/index.tsx` | 列表页 |
| `src/pages/app/system/external-demo-contact/components/ExternalDemoContactDrawer.tsx` | 新建/编辑抽屉 |
| `src/pages/app/system/external-demo-contact/constants.ts` | 状态映射、金额校验 |
| `src/utils/external-source.ts` | 外部源 reason 常量、`getErrorReason` |
| `src/locales/{zh-CN,en-US}/_modules/external-demo-contact.json` | 页面文案 |

和普通 CRUD 页面只有下面几处不同。

### 1. id 是字符串

外部表的主键类型由客户决定，proto 里一律是 `string`。前端不要 `Number(id)`，也不要按数字比较或排序。`useUpdateXxx` 的签名写成 `{ id: string; values }`，不要照抄普通模块的 `id: number`。

### 2. 金额是 decimal 字符串

- 传输和展示都用字符串，原样显示，不做 `Number()`、`toFixed()` 或加减。
- 表单用 `ProFormText` 加正则校验，不用 `ProFormDigit`。数字框会把值转成 number，超过 2^53 或小数位多时会丢精度。
- 正则的位数和外部列一致。Demo 是 `decimal(18,2)`，对应 `AMOUNT_PATTERN = /^-?\d{1,16}(\.\d{1,2})?$/`。
- 用户清空金额时不传值（`amount: undefined`）。更新时 `updateMask` 里仍有 `amount`，后端会把它置空。

### 3. 外部源错误

请求层抛出的是后端响应体 `{ code, reason, message }`，其中 `message` 已经换成 reason 的译文（`common.json` 的 `request.reason.EXTERNAL_SOURCE_*`）。用 `getErrorReason(error)` 取 reason 做判断。

| reason | 页面处理 |
|---|---|
| `EXTERNAL_SOURCE_NOT_CONFIGURED` | 列表 `request` 里**只**接住这一个：`console.warn` 带上原始错误，返回空数据并 `success: true`，再用 `locale.emptyText` 显示配置说明，告诉管理员要配置 `data.external_sources.<name>`。缺配置是部署问题，不是加载失败，所以不显示「重试」。 |
| `EXTERNAL_SOURCE_UNAVAILABLE` / `EXTERNAL_SOURCE_TIMEOUT` | 列表不处理，照常抛给 `ListTable`，由它显示译文和「重试」。 |
| `EXTERNAL_SOURCE_READ_ONLY` | 增、删、改的 `onError` 照常写 `message.error(error.message)`，提示就是「外部数据源为只读，不能修改数据」。不用另写代码。 |

除了「未配置」，列表 `request` 不要自己 catch，规则和普通页面相同（见 `docs/design-language.md`「列表加载态」）。

### 4. 页面顶部的说明

在 `ContentContainer` 里、`ListTable` 上方放一行 `<Alert style={{ marginBottom: 16 }} type="info" showIcon title={t('notice')} />`，写清楚数据来自哪个外部数据源。只写一句。

### 5. 菜单、权限和检查脚本

- 权限码的资源名和后端接口同步生成的一致。Demo 用 `external-demo-contact:create|edit|delete`。
- 路由 `path` 和后端菜单的 path 一致（`/system/external-demo-contacts`）。后端菜单的 `component` 写 `app/system/external_demo_contact/index.vue` 也能匹配上，`normalizeViewPath` 会把下划线换成横线。
- 新列表页要加进 `scripts/check-list-heights.mjs` 的 `LIST_ROUTES`。`tests/backend-menu.test.ts` 和 `tests/menu-sync.integration.test.ts` 的页面计数也要跟着改。
