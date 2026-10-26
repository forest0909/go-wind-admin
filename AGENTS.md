# AGENTS.md — go-wind-admin 开发指南（AI Agent 入口）

> 本仓库是单租户管理端：Go 后端 + React 前端（`frontend/admin/react`）。没有第二套前端，没有 AI 模块。租户、套餐、配额的代码还在，侧栏菜单已隐藏。

本文件是仓库入口：布局、门禁、铁律。前端规范见 `frontend/admin/react/AGENTS.md`。

## 仓库布局

```
backend/                    Go + Kratos + Ent（DI 手写装配 wiring_*.go，已弃用 Wire；HTTP :7788，SSE 网关 :7789）
  third_party/              收编进来的依赖源码。来源和版本见各目录 VERSION.md，许可证见仓库根 THIRD_PARTY_LICENSES
  api/protos/               业务 proto，以及收进来的 pagination、redact、conf
frontend/admin/react/       React 19 + antd 6 + ProComponents + TanStack Query + zustand（dev 端口 15888）
docs/                       文档（总入口 docs/README.md）
```

## 门禁

在声称完成之前跑完。前端 lint 不能比 `docs/plans/react-lint-baseline.tsv` 多（当前基线 909 条：659 error，250 warning）。

| 范围 | 命令 | 在哪执行 |
|---|---|---|
| 后端编译 / 静态检查 / 测试 | `go build ./...`、`go vet ./...`、`go test ./...` | `backend/` |
| 生成链 | `make api && make openapi && make ts && make ent`，之后生成目录不应再有未解释的 diff | `backend/` |
| 前端 | `npm run typecheck`、`npm run build`、`npm run lint` | `frontend/admin/react` |
| 上游依赖回流 | `bash scripts/check-no-upstream-deps.sh` | 仓库根 |

**门禁出现任何新报错，一律当作自己引入的 bug 修复。**

## 全仓铁律

1. **不吞错**：任何 catch 至少二选一——`console.error/warn` 带出**原始错误对象**，或重新抛出。用户可见的通知/Message ≠ 日志（只有翻译文案）。合法裸 catch 仅限纯本地 best-effort 兜底且注释写明原因。
2. **搜索条件一律 contains 而非 EQ**、ID 类字段不进模糊搜索；CRUD 请求体必须包 `{ data: {...} }`，但**仅限 CRUD**——`body: "*"` 的自定义 RPC（如 `internal-message/send`）收的是扁平请求体，多包一层 `data` 会被 protojson 当未知字段丢掉，接口照样 200、字段全为空。细节见 `docs/crud_module_guide/`。
3. **`backend/third_party/` 是本仓代码**，可以直接改。改了要在对应 `VERSION.md` 里记一笔。不要再 `go get` 回 `github.com/tx7do`。

## 只维护 React

新功能只做 React。CRUD 手册在 `docs/crud_module_guide/`（`backend.md` 和 `react.md`）。

**代码生成**：在 `backend/` 下用 Makefile。产物仍须按本仓铁律验收（`{ data: {...} }` 包裹、contains 搜索、已部署实例的新端点走管理页「接口同步」登记进 Api 表、`make openapi` 后再重启，`make ts` 生成 React 客户端）。

> 系统默认数据（admin 用户、角色、菜单、权限、语言等）由服务启动时在 Go 侧自动播种（`pkg/constants/default_data.go` + 各 service 的 count==0 守卫；Api 表仅在空表时于启动期自动同步，**已部署实例新增端点须在管理页「接口同步」手动触发全量重建**，否则租户闸门 fail-closed 403；「接口同步」重建读的是**打进二进制的** `backend/app/admin/service/cmd/server/assets/openapi.yaml`，所以新 proto 必须先 `make openapi` 再重启进程，否则同步"成功"而新端点依旧不在表里。新菜单同理：`count==0` 守卫意味着老库要靠「菜单同步」(MERGE) 才会多出这一行），**不要**找 SQL 种子脚本，`backend/sql/` 下只剩演示数据。

## 后端任务：用 Makefile

在 `backend/` 下执行。本仓不再使用 gow。

| 任务 | 命令 |
|---|---|
| 运行服务 | `make -C app/admin/service run` |
| Ent 生成 | `make ent` |
| Proto / API Go 代码 | `make api`（同时生成配置 pb） |
| OpenAPI | `make openapi` |
| React 的 TS 客户端 | `make ts` |
| 安装 protoc 插件 | `make plugin`（redact 和 typescript-http 从 `third_party/tools` 编译） |

项目已弃用 Wire（DI 为手写 `wiring_*.go`），不要在新代码里引入 wire 依赖。

从数据库表脚手架生成 CRUD 的外部工具不在本仓。新模块按 `docs/crud_module_guide/` 写。

## 本地验证要点

- 后端起在 `:7788`（`make -C app/admin/service run`；启动方式见 `docs/windows-startup-guide.md` / `docs/backend_deploy.md`）。前端 dev 端口 15888，代理已配置好 API 转发。
- 登录账号：全新环境播种为 `admin / Abcd@1234`（`pkg/constants/default_data.go` 的 `DefaultUserPassword`）。图形验证码的答案可在 Redis 中按 `gowind:captcha:<captchaId>` 直接读取，便于自动化验证。
- `go test ./...` 偶发 `fork/exec %TEMP%\go-build...\x.test.exe: Access is denied.`（Windows 上对刚链接好的测试二进制执行被拦，疑似安全策略/杀软实时扫描）：属环境问题、**不是代码失败**。复验办法是绕开 Temp 执行——`go test -c -o <工作区内路径>/x.test.exe ./pkg/x` 后直接跑该 exe；判成"测试挂了"之前先这样确认一次。

## 文档索引

下面这些文档还在，而且对应的代码还在。已删除模块（AI、SSO、MFA、通知、监控、脚本、程序内备份、GORM 平行后端）没有单独索引。

- **文档总入口**：`docs/README.md`；教程在 `docs/tutorial/`
- 前端规范：`frontend/admin/react/AGENTS.md`
- **表单抽屉关闭守卫**：`docs/form_drawer_close_guard.md`
- 后端：`docs/backend_project_struct.md`、`docs/backend_deploy.md`、`docs/audit-log-producer-design.md`
- 前端权限模型：`docs/frontend_authority.md`
- 查询/分页规则：`docs/list_query_rule.md`
- **改一个字段速查**：`docs/field_change_guide.md`
- **新增 CRUD 模块**：`docs/crud_module_guide/`（`backend.md`、`react.md`）
- 认证与令牌链路：`docs/authentication.md`
- 租户隔离（菜单已隐藏，代码还在）：`docs/tenant_isolation.md`
- 套餐与配额（菜单已隐藏，代码还在）：`docs/plan_billing.md`
- 任务调度：`docs/task_system.md`
- SSE：`docs/sse_architecture.md`
- 数据权限范围：`docs/data_scope_design.md`
- 设计语言：`docs/design-language.md`（改 React 的颜色、圆角、布局尺寸时先看这里）
