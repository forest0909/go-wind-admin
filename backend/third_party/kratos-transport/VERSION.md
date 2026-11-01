# kratos-transport

收编日期：2026-10-26。来源是本机 Go module cache，不是 GitHub 上的最新提交。

| 模块 | 版本 |
|---|---|
| github.com/tx7do/kratos-transport/broker | v1.3.3 |
| github.com/tx7do/kratos-transport/tracing | v1.1.2 |
| github.com/tx7do/kratos-transport/transport | v1.3.4 |
| github.com/tx7do/kratos-transport/transport/asynq | v1.3.14 |
| github.com/tx7do/kratos-transport/transport/keepalive | v1.3.5 |
| github.com/tx7do/kratos-transport/transport/sse | v1.3.8 |

本仓修改：import 路径改为 `go-wind-admin/third_party/...`。回环网卡测试在 macOS 上使用 `lo0`。`transport/asynq/server_test.go` 会一直等操作系统信号，加了 `integration` 构建标签，默认测试不跑。`transport/sse` 里两处等信号的调用已去掉。事件日志只在调用方没设 id 时才生成 UUID。keepalive 的 `Start` 会一直阻塞，测试改为后台启动后再 `Stop`。传输 Kind 的测试改为普通相等比较，避免无类型常量和 `transport.Kind` 被 `reflect.DeepEqual` 判成不相等。

LICENSE 版权人是 FlyPig，许可证仍是 MIT。


`transport/sse` 新增 `Server.CloseStream(streamID)`：关闭流并断开其全部订阅者，供用户令牌全部吊销时断开其 SSE 长连接。为此一并修了三处：`Subscriber.close` 在流已关闭时不再永久阻塞在 deregister 上；`addSubscriber` 遇到已关闭的流返回 nil（`ServeHTTP` 回 503 让客户端重连重新鉴权），不再阻塞在 register 上；连接断开后的自动清理按流指针 `Remove`，不再按 ID 误删客户端重连出的同名新流。测试 `close_stream_test.go`。
