# kratos-authn

收编日期：2026-10-26。来源是本机 Go module cache，不是 GitHub 上的最新提交。

| 模块 | 版本 |
|---|---|
| github.com/tx7do/kratos-authn | v1.1.11 |
| github.com/tx7do/kratos-authn/engine/jwt | v1.1.11 |

本仓修改：import 路径改为 `go-wind-admin/third_party/...`。`engine/metadata.go` 的 `status.Errorf` 改为常量格式串，避免 `go vet` 报 non-constant format string。错误文案不变。

