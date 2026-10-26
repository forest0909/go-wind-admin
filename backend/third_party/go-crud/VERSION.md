# go-crud

收编日期：2026-10-26。来源是本机 Go module cache，不是 GitHub 上的最新提交。

| 模块 | 版本 |
|---|---|
| github.com/tx7do/go-crud/api | v0.0.7 |
| github.com/tx7do/go-crud/audit | v0.0.3 |
| github.com/tx7do/go-crud/cache | v0.0.2 |
| github.com/tx7do/go-crud/entgo | v0.0.57 |
| github.com/tx7do/go-crud/pagination | v0.0.16 |
| github.com/tx7do/go-crud/viewer | v0.0.8 |

本仓修改：import 路径改为 `go-wind-admin/third_party/...`。`pagination.pb.go` 的 `go_package` 描述符长度已按新路径更新。字段说明里的上游 README 链接保持原样。

2026-10-26：`entgo/rule/system.go` 的 `PermissionRule` 去掉 `IsPlatformContext()` 放行，只保留 `IsSystemContext()`。单租户下 `tenant_id == 0` 不是数据范围特权；平台管理员靠令牌里的 `ALL` 在规则内部放行。同文件的 `OwnerOnlyRule`、`SoftDeleteRule`、`DenyFieldsMutationRule`、`LimitFieldAccessRule` 未改。

