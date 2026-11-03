# 品牌标识（临时）

产品名：中文「林间 Admin」，英文 / 仓库名 `linjian-admin`，出品方「林间代码」。

当前 logo 是**临时的**：两棵树代表「林」，中间留出的空隙代表「间」，主色取 `docs/design-language.md` 的品牌蓝 `#006BE6`。等设计师出了正式 logo，按下表替换即可。

原项目 go-wind-admin 的「GoWind / 风行」品牌和旋涡 logo 属于原作者，本仓库已不再使用。

## logo 在哪

| 文件 | 用在哪里 | 替换方法 |
|---|---|---|
| `frontend/admin/react/public/logo.svg` | 侧栏左上角、浏览器标签页图标（`index.html` 的 `<link rel="icon">`）、三份 README 顶部 | 直接覆盖同名文件。正方形，建议带底色，32px 下仍能看清 |
| `frontend/admin/react/src/components/bussiness/AuthLayout/icons/SloganIcon.tsx` | 登录页 / 注册页左侧的大插画 | 换成新插画的 SVG（JSX）；颜色用 `src/styles/global.css` 的 `--brand-*` 令牌，亮暗两态都要看一遍 |

改名相关的文字在这些地方：

- 标签页标题：`frontend/admin/react/.env` 的 `VITE_APP_TITLE`
- 侧栏站名、版权公司名：`frontend/admin/react/src/core/preferences/config/default.ts`（`app.name`、`copyright`）
- 登录页大标题：`frontend/admin/react/src/locales/*/_core/auth.json` 的 `systemTitle`
- Swagger 标题：`backend/api/protos/admin/service/v1/admin_doc.proto`（改完 `make openapi`）和 `rest_server.go` 的 `swaggerUI.WithTitle`
- 邮件 / 短信标题：`backend/pkg/mailtext/mailtext.go`

## 换 logo 时注意

- 如果新 logo 不是 SVG（比如 PNG），把 `default.ts` 里的 `logo.source` 和 `index.html` 的 favicon 一起改掉。老用户浏览器里存着偏好设置，`logo.source` 改了文件名就要在 `src/core/preferences/store/index.ts` 的 `migratePreferences` 里加一版迁移，否则老用户的侧栏 logo 会 404。
- 只换图、文件名不变时不需要迁移。
