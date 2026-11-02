# go-utils

收编日期：2026-10-26。来源是本机 Go module cache，不是 GitHub 上的最新提交。

| 模块 | 版本 |
|---|---|
| github.com/tx7do/go-utils | v1.1.42 |
| github.com/tx7do/go-utils/aggregator | v0.0.5 |
| github.com/tx7do/go-utils/auditutil | v0.1.0 |
| github.com/tx7do/go-utils/authorizer | v0.1.0 |
| github.com/tx7do/go-utils/captcha | v0.0.4 |
| github.com/tx7do/go-utils/converter | v0.1.0 |
| github.com/tx7do/go-utils/copierutil | v0.0.8 |
| github.com/tx7do/go-utils/crypto | v0.0.4 |
| github.com/tx7do/go-utils/fieldperm | v0.1.0 |
| github.com/tx7do/go-utils/geoip | v1.1.8 |
| github.com/tx7do/go-utils/id | v0.0.6 |
| github.com/tx7do/go-utils/jwtutil | v0.0.3 |
| github.com/tx7do/go-utils/mailer | v0.1.1 |
| github.com/tx7do/go-utils/mapper | v0.0.3 |
| github.com/tx7do/go-utils/netutil | v0.1.0 |
| github.com/tx7do/go-utils/notify | v0.0.6 |
| github.com/tx7do/go-utils/ossutil | v0.1.0 |
| github.com/tx7do/go-utils/password | v0.1.0 |

本仓修改：import 路径改为 `go-wind-admin/third_party/...`。`captcha/example_test.go` 的示例函数名按 `go vet` 的 Example 规则改过，并加了 `example` 构建标签（示例会连本机 Redis，默认测试不跑）。`auditutil/testpb` 和 `fieldperm/testpb` 的 `go_package` 描述符长度已按新路径更新。`id.Increase` 改成原子自增，避免并发读到同一个序号；计数仍然在 1000 处回绕。GeoLite 测试期望的省份改为数据库实际返回的「四川」。`trans` 和订单号碰撞测试不再用会随时间抖动的严格相等。

`captcha` 的算术验证码（`DriverMath`）不再走上游 `base64Captcha.DriverMath`：上游每个字符随机用花体字、背景铺满数字噪点，还没有除法。改为 `captcha/math.go` 自绘——加减乘除（结果均为非负整数，除法整除），单一字体、浅色干扰线；校验时去掉输入前后空格。

`go-utils/id/machineid` 自带 MIT 许可证（Copyright 2017 Denis Brodbeck），LICENSE.md 随包保留。

未收编 OPA 引擎和 zanzibar。`authorizer.newEngine` 删掉这两个分支；未知类型和空类型不再退回 noop，记错误并返回 nil。显式 `noop` 仍可用。

`converter.ApiPermissionConverter` 加了 `WithResourceAliases`：按路径前缀（整段、最长优先）指定资源名或完整编码，给接口挂在模块前缀下（如 `internal-message/...`）的情况用。不设别名时行为不变。

`captcha.Verify` 改为一次性领取：用 Lua 原子地 GET+DEL 答案（不用 GETDEL，兼容 Redis 6.2 以前），答错也作废。此前答错不删、答对后才非原子 DEL，可对同一 id 穷举 0..81，并发提交同一正确答案能多次通过。`TestVerify_WrongAnswer` 期望随之改为验证码已不存在；新增 miniredis 测试 `captcha_consume_test.go`。

`netutil.SetTrustedProxies` 先把默认初始化标记为已完成：此前启动时调用它之后，首个请求里的 `initTrustedProxies` 仍会用默认网段覆盖配置（默认初始化写全局变量时也没加锁）。测试 `TestSetTrustedProxies_NotOverriddenByLazyDefault`。

`authorizer` 加了 `SetResetWrapper` / `ReloadPolicies`：`ResetPolicies` 设置了包装函数时经它调用实际装载（admin 用它在装载前递增 Redis 版本号、装载后广播，让其他实例重载）；`ReloadPolicies` 只重载本实例。实际装载加了互斥锁，并发装载不会让旧快照覆盖新快照。
