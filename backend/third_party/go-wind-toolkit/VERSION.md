# go-wind-toolkit

收编日期：2026-10-26。来源是本机 Go module cache，不是 GitHub 上的最新提交。

| 模块 | 版本 |
|---|---|
| github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact | v0.0.0-20260831125122-5bb4931991b2 |

本目录只放运行时包 `protoc-gen-go-redact/redact/v1`（生成代码会 import 它）。插件本体在 `third_party/tools/protoc-gen-go-redact`。

许可证是 **Apache-2.0**，不是其余依赖所用的 MIT。LICENSE 与 NOTICE 原样保留。NOTICE 写明这是 Shivam Rathore 的 protoc-gen-redact 的衍生作品。

本仓修改：import 路径和 `redact.proto` 的 `go_package` 从 `github.com/tx7do/go-wind-toolkit/...` 改为 `go-wind-admin/third_party/go-wind-toolkit/...`。`redact.pb.go` 里对应的原始描述符按新路径改了字符串长度，并同步加长外层 FileOptions 的长度。
