# OIDC SSO 演练环境（Keycloak）

本目录提供一套**本地可复现**的企业 SSO 演练环境：单容器 Keycloak + 一键初始化脚本，配合 GoWind Admin 的 OIDC SSO 登录（见 docs/authentication.md「OIDC SSO」一节）即可完整演示企业账号登录。

## 快速开始

```bash
# 1. 拉起 Keycloak（start-dev 演练模式，端口 8081）
docker compose -f docker-compose.keycloak.yml up -d
# 等待 ~30s 就绪

# 2. 一键初始化（realm gwa / 机密客户端 gwa-admin / 测试用户）
node setup-keycloak.mjs

# 3. 后端环境变量（启动前设置）
export SSO_OIDC_ISSUER=http://localhost:8081/realms/gwa
export SSO_OIDC_CLIENT_ID=gwa-admin
export SSO_OIDC_CLIENT_SECRET=gwa-secret
export SSO_OIDC_REDIRECT_URL=http://localhost:15888/auth/sso/callback
export SSO_AUTO_CREATE=true   # 首次 SSO 登录按 email 自动预置用户（无密码凭证）
# 然后启动后端

# 4. 浏览器打开前端登录页 → 点「使用企业账号登录」→ Keycloak 登录页
#    账号 sso-user / Keycloak@123 → 回调后自动进入系统
```

## 已验证内容

真实 Keycloak 26.0 全流实测通过（2026-10-03）：discovery 解析、授权 URL state 签发、
登录表单流（cookie/302 链）、code 交换、userinfo 映射、JWT 签发、state 重放拒绝、
自动预置用户落库。e2e 脚本与揭出的修复（SsoLogin 隐私层绕过、user_repo nil mask
守卫）见提交 7d65e097。

## 注意

- **仅演练用**：start-dev 模式 + H2 内嵌库，容器删除即失；生产用独立 Keycloak 集群；
- redirect_uri 含三端开发端口（react 15888 / ele 15777 / vben 15666，2026-09-30 迁 15xxx 后口径；15889 已归 go-wind-iot、15890 已归 go-wind-im，勿再登记），按实际端口调整；
- `SSO_AUTO_CREATE=true` 预置的用户**无密码凭证**（不能密码登录）且**零角色**
  （fail-closed 403）——需管理员在用户管理里挂角色后方可进入后台。
