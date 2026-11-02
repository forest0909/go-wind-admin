# Add CRUD Module (go-wind-admin)

> **定位**：本目录是「新增完整 CRUD 业务模块」的参考层手册：执行顺序与通用陷阱在本页；
> 后端全流程与样例镜像索引在 [backend.md](./backend.md)，React 页面实现在 [react.md](./react.md)。
> 数据在客户已有的业务库（外部数据源：PostgreSQL / MySQL / SQL Server，不走 ent）时，按 [external.md](./external.md) 写。
> 范围划界：给**已有**资源加字段走 [field_change_guide.md](../field_change_guide.md)；修 bug、重构、
> 改既有模块逻辑不在本手册范围。教程层[第 4 章](../tutorial/04-first-crud-module.md)是本手册的路线图视角。
> 正文为英文原稿，保留原文以避免转译失真。

This guide walks through scaffolding a **complete CRUD business module** across the backend and the React admin frontend (`frontend/admin/react`), following the project's real (not documented-aspirational) patterns. There is one backend and one frontend; the former Vue frontends were removed.

## The three-layer architecture in one breath

```
Backend (always first):  Proto (domain + BFF) → Service → Data/Repo → Server → hand-written wiring (cmd/server/wiring_ent.go)
Frontend (depends on generated types):  generated → hooks → pages → router/i18n
```

**Hard rule:** the frontend `generated/` types come from the backend's codegen: `make api` emits only the **Go** code (`backend/api/gen/go/`), `make ts` emits the React TypeScript client (`backend/api/buf.react.admin.typescript.gen.yaml` → `frontend/admin/react/src/api/generated/`). The frontend cannot be completed before the backend proto + codegen is done. Always do backend first (or at least through Step 3 of the backend flow).

## Step 0 — Clarify requirements (once, up front)

Before writing anything, ask the requester (batch all of them into one round) for:

1. **Entity name** in three forms: PascalCase (`Product`), snake_case (`product`), and Chinese label (`商品`).
2. **Core fields** — a list of `{name, type, enum?, required?}`. Distinguish business fields from the audit fields (created_by/updated_by/deleted_by + timestamps) which are added automatically by Ent mixins and need not be listed.
3. **Functional domain** — does it belong to an existing domain (`permission`, `dict`, `identity`, `opm`, `system`, `log`, …) or is it a brand-new domain? This decides whether you edit an existing proto package / route file or create a new one.
4. **Module shape** — plain CRUD, tree (parent_id), or master-detail (two linked tables like dict_type+dict_entry). Modules need no tenant handling: `tenant_id` is a legacy column fixed at 0. Plain CRUD is the default; the other shapes extend the chapter templates.

Only proceed once you have these answers.

## Step 1 — Execution order (the rule that prevents 80% of mistakes)

```
1. Backend  →  read backend.md, execute Steps 1–11
                 (ends with `make api` having produced generated/*.pb.go AND
                  `make ts` having regenerated the React generated/ TypeScript)
2. Frontend →  read react.md and execute its steps
                 frontend steps DEPEND on the regenerated apiClient.<entity>Service existing
```

If the user insists on frontend-only for an entity that has no backend proto yet, stop and explain the dependency: the frontend's `apiClient.<entity>Service` getter is *generated* from the backend proto, so there is nothing to call without it.

## Step 2 — Backend

Read **`backend.md`** in full before touching backend files. It covers the 11-step flow with real sample references:

```
1. domain proto  → 2. BFF proto  → 3. make api (+openapi)
4. ent schema    → 5. make ent
6. repo          → 7. service  → 8. register server
9. make register ENTITY=<entity> → 10. make build verify
11. seeds & the Api registry (count == 0 guards; the Api table and permission points re-sync on every restart)
```

Real samples to mirror: `backend/api/protos/dict/service/v1/dict_type.proto`, `backend/api/protos/admin/service/v1/i_api.proto`, `backend/app/admin/service/internal/data/api_repo.go`, `backend/app/admin/service/internal/service/dict_type_service.go`. **Read the matching sample file before writing your version** — copy its structure, change names/fields.

After backend Step 3 (`make api` → Go only), regenerate the frontend TypeScript client — there is no `generate:api` npm script; the TS codegen lives in `backend/Makefile`'s `ts:` target:

```bash
cd backend && make ts
```

It writes only into `frontend/admin/react/src/api/generated/`. Afterwards check the generated index (`src/api/generated/admin/service/v1/index.ts`) has the new `get <entity>Service()` getter.

## Step 3 — Frontend

Read **`react.md`** and follow its step list (ProTable list, DrawerForm + formRef, refresh via `queryClient.invalidateQueries(['listXxx'])`). It documents the *actual* project pattern; trust it over older prose elsewhere when they disagree.

## Step 4 — Completion checklist

Before declaring done, verify cross-cutting concerns:

- [ ] Backend compiles: `cd backend && make build` (do **not** use `make gen` as the check — `gen: ent api openapi` only regenerates code, it never compiles)
- [ ] `go test ./...` passes. Three guards there catch the silent wiring mistakes: `TestEveryOpenAPIOperationHasRoute` (an endpoint in openapi without a `Register*HTTPServer` call → 404), `TestRestServicesImplementAllMethods` (a service method left to the embedded interface → nil panic / 500), `TestRepoUpdateRoundTrip` (add a case for your repo: a field in `updateMask` that the Update set-callback forgets → 200 but not written)
- [ ] Swagger at `http://localhost:7788/docs` shows the new resource with correct routes
- [ ] List loads, search filters, pagination works, create/edit form validates and persists, update actually changes fields, delete confirms and refreshes
- [ ] No edits to any `generated/` directory (these are regenerated, not hand-edited); dependency wiring lives in `cmd/server/wiring_ent.go` (hand-written, no codegen)
- [ ] `apiClient.<entity>Service` getter exists in the frontend generated index (proves proto round-trip worked)
- [ ] i18n: no hardcoded Chinese/English in pages — everything goes through `t` with keys in the locale JSONs. A new error proto reason needs a `request.reason.<REASON>` translation in both locales' `_core/common.json` (`node scripts/check-error-reason-i18n.mjs`), otherwise the user only sees the generic "request failed" text
- [ ] Update operations carry `updateMask` (frontend `useUpdateXxx` does this automatically via `makeUpdateMask`; do not hand-build the mask)
- [ ] If the entity carries association ID lists (M2M/O2M edges): repo Update follows the snapshot + blacklist + Replace pattern from `backend.md` Step 6, and association edits round-trip in the UI (add / clear-all / re-add)
- [ ] Api registry: `make openapi` before restarting — the Api table and permission points auto-sync at boot from the embedded `openapi.yaml`; `go test ./...` must pass `TestPermissionCodeConsistency` (button codes ↔ APIs ↔ pages)
- [ ] Menu entries for the new module: the sidebar comes from the backend menu table (`accessMode: "backend"`), not from the React route files. On an existing deployment click 菜单管理 →「同步菜单」 after adding the route (it merges the React route files into the menu table by path), or add the row by hand; editing `pkg/constants/default_data.go` (`DefaultMenus`) only affects empty-DB fresh installs (all seeds are `count == 0` guarded, see `backend.md` Step 11)

## Step 5 — Pitfalls (memorize these)

1. **`PaginationQuery` must be instantiated with `new`.** Its `.toRawParams()` depends on instance getters. Passing a plain object literal and calling `.toRawParams()` crashes. Always `new PaginationQuery({ paging, formValues })`.

2. **Every Update call must carry `updateMask`.** The backend's `UpdateX` runs the DTO through `FilterByFieldMask` first, which **clears every field not listed in the mask** — only surviving fields reach the `SetNillable*` mapping and the SQL SET clause. An omitted/nil mask therefore means **no filtering at all: every populated field in the submitted DTO gets written**, so a stale or partial form round-trip silently corrupts untouched columns. `useUpdateXxx` calls `makeUpdateMask(Object.keys(values))` internally. Use it; do not call `apiClient.<entity>Service.Update` directly with a hand-built mask. If the entity carries association ID lists (M2M/O2M edges), the repo additionally needs the snapshot + blacklist + Replace pattern — see `backend.md` Step 6 before writing the repo.

3. **Never hand-edit `generated/`.** Backend `api/gen/go/` and the frontend's `api/generated/` are produced by codegen. If a type or service is missing, the fix is to regenerate (fix the proto, rerun `make api` for Go / `make ts` for the frontend TypeScript), not to edit the generated file.

4. **Backend error codes: Service layer uses `adminV1.ErrorXxx`.** Repo layer may use domain errors (`permissionV1.ErrorXxx`, `dictV1.ErrorXxx`), but the Service (BFF) layer standardizes on `adminV1.ErrorBadRequest` / `adminV1.ErrorInternalServerError` from `protos/admin/service/v1/admin_error.proto`. Don't use `errors.New`. The frontend shows only the translated `reason`, never the backend `message`.

5. **Refresh after mutations:** `queryClient.invalidateQueries({ queryKey: ['listXxx'] })` (prefix match).

6. **i18n files auto-register via `import.meta.glob` — do not manually register them.** The *route menu title* lives in a separate file: `meta.title: 'routes:xxx'` — the colon is the i18next **namespace** separator; keys live in `src/locales/<lang>/_core/routes.json`.

7. **No error swallowing.** Every `catch` must either log the **original error object** (`console.error(...)`/`console.warn(...)`) or rethrow it. A user-visible notification/Message is NOT logging. Bare `catch {}` is acceptable only for pure local best-effort fallbacks, with a comment explaining why.

8. **Regenerated `*_http.pb.go` files churn.** `protoc-gen-go-http` numbers handler functions with a counter shared across the whole `admin/service/v1` package (`_RoleService_List17_HTTP_Handler`), so adding one BFF service renames handlers in unrelated files (`i_role_http.pb.go`, `i_user_http.pb.go`, …). That diff is expected — commit it. When two branches each add a service, do not hand-merge these files: take either side, then rerun `make api` on the merged protos and commit the regenerated output.

## When to read which chapter

| Situation | Read |
|---|---|
| Any backend work | `backend.md` |
| Frontend pages | `react.md` |
| Data lives in a customer-owned database (external source, not ent) | `external.md` |
| Tree / master-detail shape | The base chapter, plus inspect a real sample of that shape (e.g. `system/dict/` for master-detail, menu module for tree) |
