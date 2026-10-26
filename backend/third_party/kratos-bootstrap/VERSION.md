# kratos-bootstrap

收编日期：2026-10-26。来源是本机 Go module cache，不是 GitHub 上的最新提交。

| 模块 | 版本 |
|---|---|
| github.com/tx7do/kratos-bootstrap/api | v0.0.45 |
| github.com/tx7do/kratos-bootstrap/bootstrap | v0.1.17 |
| github.com/tx7do/kratos-bootstrap/cache/redis | v0.1.2 |
| github.com/tx7do/kratos-bootstrap/config | v0.2.3 |
| github.com/tx7do/kratos-bootstrap/database/ent | v0.1.6 |
| github.com/tx7do/kratos-bootstrap/logger | v0.1.3 |
| github.com/tx7do/kratos-bootstrap/oss/minio | v0.1.3 |
| github.com/tx7do/kratos-bootstrap/registry | v0.2.3 |
| github.com/tx7do/kratos-bootstrap/rpc | v0.1.3 |
| github.com/tx7do/kratos-bootstrap/tracer | v0.1.5 |
| github.com/tx7do/kratos-bootstrap/transport/asynq | v0.0.7 |
| github.com/tx7do/kratos-bootstrap/transport/sse | v0.0.5 |

本仓修改：import 路径改为 `go-wind-admin/third_party/...`。`api/gen/go/conf/v1` 各 pb 的 `go_package` 描述符长度已按新路径更新。`app_info.pb.go` 的 FileOptions 长度从 1 字节 varint 变成 2 字节。`bootstrap` 测试在缺少上游 `../../configs` 时跳过。白名单测试改成对齐现有匹配方向：名单里是方法名时，完整操作名也能命中；名单里是完整操作名时，不会反查方法名。

