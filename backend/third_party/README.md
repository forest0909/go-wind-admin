# third_party

这里的代码已经收进主模块，import 路径是 `go-wind-admin/third_party/...`。这是本仓自己的代码，可以直接改。

改过行为或注释之后，在对应目录的 `VERSION.md` 里记一笔：改了什么、为什么。不要再 `go get` 回原来的上游模块。仓库根的 `scripts/check-no-upstream-deps.sh` 会拦住 `github.com/tx7do`、`buf.build/tx7do`、`buf.build/go-wind`。

来源版本写在各目录的 `VERSION.md`。许可证原文在仓库根的 `THIRD_PARTY_LICENSES`，各目录里的 `LICENSE` 也保留着。

`tools/` 放 `protoc-gen-go-redact` 和 `protoc-gen-typescript-http`。redact 的运行时包在 `go-wind-toolkit/protoc-gen-go-redact/redact/v1`，不在插件目录里再放一份 Go 源码。插件测试用的 `redact/v1/redact.proto` 是指向运行时 proto 的符号链接。
