<div align="center">

<img src="docs/brand/vortex-tile.svg" width="120" alt="GoWind Admin" />

# GoWind Admin

**Out-of-the-box, enterprise-grade full-stack admin scaffold (Go backend + React 19 frontend)**

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Frontend](https://img.shields.io/badge/Frontend-React_19_only-61DAFB?logo=react)](./frontend/admin/react)
[![React](https://img.shields.io/badge/React-19.x-61DAFB?logo=react)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com/)

[English](./README.en-US.md) | [中文](./README.md) | [日本語](./README.ja-JP.md)

</div>

---

## Project Highlights

- **Frontend: React 19 + Ant Design only**. The directory is `frontend/admin/react`. There is no second frontend
- **Access control**: roles, departments, menus, buttons, and data scope. API authorization uses Casbin. OPA is not included
- **Security**: login logs and operation logs, password policy, image captcha, login rate limiting, and password encryption. See [Security & Compliance](#security--compliance-mlps-20)
- **Microservice + Monolith**: Built on the go-kratos microservice framework, yet supports monolith-mode development and deployment — flexible for any team size
- **Full-stack Code Generation**: Protobuf → Go API / TypeScript clients, Ent Schema → ORM. Under `backend/`: `make api`, `make ent`, `make ts`.
- **Production-ready**: JWT auth, SSE push, async task scheduling, Swagger docs, one-click Docker deployment
### What this tree is

**A single-tenant admin: Go backend + React frontend.** There is no second frontend and no AI module.

Tenant, plan, and quota code is still in the tree. Their sidebar entries are hidden. API authorization is Casbin.

Dependencies live in `backend/third_party/`. Licenses: [THIRD_PARTY_LICENSES](./THIRD_PARTY_LICENSES). Commands: [AGENTS.md](./AGENTS.md).

---

## Start Here

- **Want to learn this codebase properly**: start from the [documentation index](./docs/README.md) (in Chinese). The docs split into a **tutorial layer** (an [8-chapter progressive tutorial](./docs/tutorial/README.md) taking you from an empty environment to building business modules and deploying safely) and a **reference layer** (the authoritative description of each subsystem), with recommended reading paths per role (full-stack adopter / backend / frontend / ops & security).
- **Just want to see it run**: the [demo sites](#demo) and the [Quick Start](#quick-start) are right below.

---

## Code Repositories

This is a trimmed working copy. It does not track the two upstream URLs below:

| Platform | URL |
|----------|-----|
| Repository | this working copy |
| Upstream Gitee | <https://gitee.com/tx7do/go-wind-admin> |
| Upstream AtomGit | <https://atomgit.com/tx7do/go-wind-admin> |

---

## Demo

This tree has no online demo. The local frontend is React only, on dev port 15888.

| Part | Where |
|---------|--------|
| Frontend | `frontend/admin/react` |
| Backend | HTTP `:7788` |
| Account | `admin` / `Abcd@1234` |

- Backend Swagger: <http://127.0.0.1:7788/docs/> after the service is up
- Default account / password: `admin` / `Abcd@1234`

---

## Quick Start

### Environment Requirements

| Tool | Version |
|------|---------|
| Go | 1.26+ (follow `backend/go.mod`) |
| Node.js | `^20.19.0 \|\| >=22.12.0` (required by Vite 8). **Do not use 21.x**, or 20.18 and below |
| pnpm | `>= 9`. The React app does not pin a pnpm version |
| Docker | 20.0+ |

### Environment Scripts

- Linux / macOS Development: `backend/scripts/env/install_unix_dev.sh`
- Linux / macOS Production: `backend/scripts/env/install_unix_prod.sh`
- Windows Development: `backend/scripts/env/install_windows_dev.ps1`

### Docker Deployment Modes

- **full_deploy**: Starts middleware + backend application together, suitable for one-click demos or production deployment.
- **libs_only (Recommended for development)**: Starts middleware only; run the application locally in your IDE for debugging.

### Backend Startup

> Backend commands run from `backend/` via the Makefile (`make api`, `make ent`, `make -C app/admin/service run`). See [AGENTS.md](./AGENTS.md).

**Linux / macOS:**

```shell
# All commands below run inside backend/ (scripts/ lives there)
cd backend

# Grant script execution permissions
# scripts/ nests three levels; a glob misses env/lib and deploy/sse, so use find
find ./scripts -name '*.sh' -exec chmod +x {} +

# Development (Recommended)
./scripts/env/install_unix_dev.sh
./scripts/docker/libs_only.sh
make -C app/admin/service run

# Production
./scripts/env/install_unix_prod.sh
./scripts/docker/full_deploy.sh

# PM2 Process Management (Advanced Production)
./scripts/deploy/pm2_service.sh
```

**Windows (PowerShell as Administrator):**

```powershell
# All commands below run inside backend/ (scripts/ lives there)
cd backend

# Allow script execution (only needed once)
Set-ExecutionPolicy RemoteSigned -Scope CurrentUser

# Initialize environment
.\scripts\env\install_windows_dev.ps1

# Local development
.\scripts\docker\libs_only.ps1
make -C app/admin/service run

# One-click full deployment
.\scripts\docker\full_deploy.ps1
```

### Frontend Startup

This tree keeps React only, at `frontend/admin/react`. There is no `package.json` at the repo root or under `frontend/admin/`; `pnpm install` there fails with ENOENT.

| Frontend Edition | Directory | Command | Port |
|------------------|-----------|---------|------|
| React | `frontend/admin/react` | `pnpm dev` | 15888 |

```shell
# Enter the React app, then install and start.
# Do not run pnpm install at the repo root or under frontend/admin/.
# API http://localhost:7788 , account admin / Abcd@1234
cd frontend/admin/react
pnpm install
pnpm dev                    # port 15888
```

> Use Node.js `^20.19.0` or `>=22.12.0`. Do not use 21.x. Package manager: pnpm.
>
> The dev port is `VITE_SERVER_PORT` in `frontend/admin/react/.env.development` (15888). Vite moves to the next port if it is busy; CORS already allows `15888` and `15889`.
>
> `make ts` in `backend/` generates the React client only.

---

## Tech Stack

<table>
<tr><th>Layer</th><th>Technologies</th></tr>
<tr><td><strong>Backend Framework</strong></td><td><code>Golang</code> · <code>go-kratos v2</code> · <code>Protobuf / Buf</code></td></tr>
<tr><td><strong>ORM</strong></td><td><code>Ent</code> · <code>MySQL</code> · <code>PostgreSQL</code></td></tr>
<tr><td><strong>Middleware</strong></td><td><code>Redis</code> (compose pulls <code>bitnami/redis:latest</code>, no version pin; the codebase uses only long-standing commands such as Set/Expire/Publish — nothing Redis-8-specific) · <code>MinIO</code> (S3-compatible object storage)</td></tr>
<tr><td><strong>Authentication & Authorization</strong></td><td><code>JWT</code> for authentication · <code>Casbin</code> for API authorization. Startup fails when the engine is not casbin</td></tr>
<tr><td><strong>Realtime</strong></td><td><code>SSE</code> (server push) · <code>Asynq</code> (async tasks)</td></tr>
<tr><td><strong>Frontend</strong></td><td>React only. See the React row below</td></tr>
<tr><td><strong>Removed</strong></td><td>No second frontend, no AI, no SSO, no MFA, and no in-app database backup</td></tr>
<tr><td><strong>Not a product surface</strong></td><td>Tenant, plan, and quota pages remain, and they are hidden from the sidebar</td></tr>
<tr><td><strong>React Edition</strong></td><td><code>React 19</code> · <code>TypeScript</code> · <code>Vite</code> · <code>Zustand</code> · <code>Ant Design V6</code> (no UMI) · <code>Tailwind CSS</code></td></tr>
<tr><td><strong>Deployment & Ops</strong></td><td><code>Docker</code> · <code>Docker Compose</code> · <code>PM2</code> · <code>Swagger UI</code></td></tr>
</table>

---

## Security & Compliance (MLPS 2.0)

Security capabilities are designed with reference to the technical requirements of China's Cybersecurity Multi-Level Protection Scheme 2.0 (MLPS 2.0, Level 2/3), ready out of the box for privacy-conscious private deployment scenarios:

| Requirement | Implementation |
|------------|----------------|
| **Security Audit** | Login logs and operation logs, recording the client IP (with geolocation) and the `X-Request-ID` request id sent by the frontend. Daily scheduled archiving via asynq: 180-day in-database retention (`AUDIT_RETENTION_DAYS`, adjustable); expired rows are exported to JSONL archive files for long-term traceability |
| **Identity Authentication** | Password complexity (≥8 chars, at least 3 of 4 character classes), password history reuse check (last 3 by default), password validity period (90 days by default) — thresholds are adjusted via the "Parameter Management" platform parameters (built-in keys seeded at startup; environment-variable configuration is deprecated); image captcha; Redis login failure rate limiting (IP + username dual dimensions) |
| **Access Control** | API authorization is Casbin. Role, permission, and API mappings live in the database and hot-reload. Menus, buttons, role data scope, and field permissions are also in place |
| **Tenant code** | Isolation and plan code remains, and the sidebar entries are hidden. This tree is used as a single-tenant admin, not delivered as multi-tenant SaaS |
| **Data Confidentiality** | Passwords for login, user creation, reset, and profile change are AES-encrypted in transit and bcrypt-hashed at rest; sensitive task configs encrypted at rest with AES-256-GCM (transparent via Ent hooks); JWT RS256 asymmetric signing; refresh token in HttpOnly Cookie; transport-layer TLS enabled at deployment (backend `server.rest.tls` config, or nginx / load balancer termination) |
| **Data Backup & Recovery** | The program does not ship a database backup. Use the database's own tools (for example `pg_dump`) or an operations platform, and rehearse a restore |
| **Frontend Security** | `frontend/admin/react/scripts/deploy/nginx.conf` sets X-Frame-Options / HSTS / Content-Security-Policy. The React build also injects a CSP `<meta>` into `index.html` |

> **Note**: MLPS evaluation covers more than technical requirements — management policies, physical environment, personnel organization, etc. This project covers the technical measures portion, providing direct support for evaluation preparation in private deployments, but it does not replace the full MLPS certification process.

---

## Features

> All list pages (business data and audit logs) support "Export" with paginated aggregation under the current filters, in CSV or XLSX format (up to 10,000 rows).

### Organization & Permissions

| Feature | Description |
|---------|-------------|
| User Management | Manage and query users with advanced search and department-linked users; enable/disable users, set/unset manager, reset password, configure multiple roles / departments / managers, one-click login as a specified user. |
| Tenant Management | The code and page remain at `/tenant/members`. The sidebar entry is hidden. This tree is not used as multi-tenant SaaS. |
| Plan & Quota Management | The code and page remain at `/tenant/plans`. The sidebar entry is hidden. |
| Role Management | Manage roles and role groups; user selection by role; menu grants, data-scope configuration (five levels / custom organization-unit sets), and field-level permissions (blacklisted field sets); batch add/remove employees. |
| Permission Management | Manage permission groups, menus, and permission points with tree-view listing. |
| Organization Management | Manage organizations with tree-view listing. |
| Position Management | Manage positions, which can label a user. Excel import does not resolve the organization column (`orgUnitId` is not an import field). |
| Menu Management | Configure directories, menus, and buttons. Menu sync exists only on React: truncate-and-rebuild, or merge by full path while keeping menu IDs and role grants. |

### System Features

| Feature | Description |
|---------|-------------|
| API Management | Manage APIs with API synchronization (mainly for picking endpoints when adding permission points), tree-view listing, and operation-log request/response configuration. |
| Dictionary Management | Manage dictionary categories and entries; category-linked entries, server-side multi-column sorting, import/export. |
| Task Scheduler | Manage tasks and task run logs; create, update, delete, start, pause, and run immediately. |
| File Management | Manage uploads: search, upload to OSS or local storage, download, copy file address, delete, and image preview (large view). |
| Account Login | Sign in with username / email / phone number, with image captcha and login rate limiting. |
| Verification delivery | Profile email and phone changes read SMTP and SMS credentials from `app/admin/service/configs/mail.yaml`. An empty SMTP host or SMS provider means that path is not configured. Sends are synchronous and do not enter the task queue. |
| Online Sessions | View currently online sessions (user, tenant, client type, login IP, User-Agent, device ID, login time), with keyword filtering, pagination (login time desc) and per-session forced logout; auto-refresh every 30s. |
| Parameter Management | Manage platform-wide system parameters as key/value pairs (distinct from business dictionaries); built-in parameters are seeded at startup and cannot be deleted; reads go through a server-side caching accessor, and in multi-instance deployments changes are broadcast over Redis pub/sub to invalidate every instance's cache. |
| Language Management | Manage supported languages: name, code, native name, enabled and default status. |

### Messaging & Logs

| Feature | Description |
|---------|-------------|
| Message Categories | Manage the message categories offered in message management. They are **flat, one level deep** (`sys_internal_message_categories` has no parent_id column, and deletion removes only the row — no tree cascade). |
| Message Management | Send by scope (all users / specified users) with revocation; broadcast fan-out runs on an async task queue (resumable, idempotent); view read status and read time. |
| Internal Mail | Each user sees only their own inbox: view, delete, mark as read, mark all as read. A platform admin can still open a chosen user's inbox from the user detail page. |
| Login Logs | Query login logs for successful and failed logins; supports IP geolocation. |
| Operation Logs | Query operation logs for normal and abnormal operations; supports IP geolocation, resource identification, and detail view. |

### Personal Center

| Feature | Description |
|---------|-------------|
| Personal Center | View and edit personal info, check last-login info, change password, bind / rebind email (verification code), etc. |
| My Active Sessions | View your own currently active sessions (login IP, device, login time, etc.); the current session is view-only, any other session can be signed out individually. |

---

## Project Structure

```
go-wind-admin/
├── backend/                        # Backend project
│   ├── api/                        # Protobuf API definitions & generated code
│   │   ├── protos/                 # .proto sources (layered by domain)
│   │   └── gen/go/                 # Go code generated by buf
│   ├── app/admin/service/          # Admin service application
│   │   ├── cmd/server/             # Entry point (main.go, wiring_ent.go DI assembly)
│   │   ├── configs/                # Configuration files (YAML). Machine overrides go in configs/local/ (gitignored)
│   │   └── internal/               # Business core (data/service/server)
│   ├── pkg/                        # Shared packages
│   │   ├── oss/                    # Object storage (MinIO)
│   │   ├── eventbus/               # Event bus
│   │   └── ...                     # Other utility packages
│   ├── scripts/                    # Deployment scripts (env/docker/deploy)
│   └── sql/                        # Demo data SQL (default data is seeded automatically at service startup)
├── frontend/admin/                 # Frontend (React only)
│   └── react/                      # React 19 + Ant Design V6
└── docs/                           # Project documentation
```

---

## Screenshots

<table>
    <tr>
        <td><img src="./docs/images/admin_login_page.png" alt="Backend user login page"/></td>
        <td><img src="./docs/images/admin_dashboard.png" alt="Backend dashboard"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_user_list.png" alt="Backend user list page"/></td>
        <td><img src="./docs/images/admin_user_create.png" alt="Backend create user page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_tenant_list.png" alt="Backend tenant list page"/></td>
        <td><img src="./docs/images/admin_tenant_create.png" alt="Backend create tenant page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_org_unit_list.png" alt="Organization unit list page"/></td>
        <td><img src="./docs/images/admin_org_unit_create.png" alt="Create organization unit page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_position_list.png" alt="Backend position list page"/></td>
        <td><img src="./docs/images/admin_position_create.png" alt="Backend create position page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_role_list.png" alt="Backend role list page"/></td>
        <td><img src="./docs/images/admin_role_create.png" alt="Backend create role page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_permission_list.png" alt="Backend permission list page"/></td>
        <td><img src="./docs/images/admin_permission_create.png" alt="Backend create permission page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_menu_list.png" alt="Backend menu list page"/></td>
        <td><img src="./docs/images/admin_menu_create.png" alt="Backend create menu page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_task_list.png" alt="Backend scheduled task list page"/></td>
        <td><img src="./docs/images/admin_task_create.png" alt="Backend create scheduled task page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_dict_list.png" alt="Backend dictionary list page"/></td>
        <td><img src="./docs/images/admin_dict_entry_create.png" alt="Backend create dictionary entry page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_internal_message_list.png" alt="Backend internal message list page"/></td>
        <td><img src="./docs/images/admin_internal_message_publish.png" alt="Backend publish internal message page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_login_audit_log_list.png" alt="Backend login audit log page"/></td>
        <td><img src="./docs/images/admin_api_audit_log_list.png" alt="Backend operation audit log page"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_api_list.png" alt="API list page"/></td>
        <td><img src="./docs/images/api_swagger_ui.png" alt="Backend built-in Swagger UI page"/></td>
    </tr>
</table>

## Companion Tools

Code generation runs under `backend/`: `make api`, `make openapi`, `make ent`, `make ts`. Run the service with `make -C app/admin/service run`. See [AGENTS.md](./AGENTS.md).

## Community & Contribution

Welcome to join the GoWind Admin community. The documents below describe how to contribute code, report issues, and disclose security vulnerabilities:

- [Contributing Guide](./CONTRIBUTING.md) — development environment, codegen conventions, commit conventions and the PR process
- [Code of Conduct](./.github/CODE_OF_CONDUCT.md) — community interaction expectations
- [Security Policy](./SECURITY.md) — vulnerability disclosure process and scope
- [Changelog](./CHANGELOG.md) — release notes
- Issue templates: [Bug Report](./.github/ISSUE_TEMPLATE/bug_report.md) · [Feature Request](./.github/ISSUE_TEMPLATE/feature_request.md)
- [PR Template](./.github/PULL_REQUEST_TEMPLATE.md)

## Contact

- WeChat: `yang_lin_bo` (note: `go-wind-admin`)
- Juejin column: [go-wind-admin](https://juejin.cn/column/7541283508041826367)

## Acknowledgements

[![JetBrains](https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.svg)](https://jb.gg/OpenSource)

Thanks to JetBrains for providing free GoLand & WebStorm open-source licenses.
