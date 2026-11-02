# 外部数据源模块

客户已经有自己的业务库，要把里面的数据接进本系统查看或维护时，按本页写。每个「接客户已有数据库」的业务模块都照这个写法做。贯穿例子是「外部数据示例」（联系人）：

| 层 | 文件 |
|---|---|
| 底层包 | `backend/pkg/extdb/`（注册表、只读防护、超时、分页适配、事务、错误） |
| proto | `backend/api/protos/external/service/v1/demo_contact.proto`、`external_error.proto`，BFF `admin/service/v1/i_external_demo_contact.proto` |
| 仓储 | `backend/app/admin/service/internal/data/external_demo_contact_repo.go` |
| 服务 | `backend/app/admin/service/internal/service/external_demo_contact_service.go` |
| 建表脚本 | `backend/sql/external_demo/{postgres,mysql,sqlserver}.sql`（只供手动执行） |
| 测试 | `external_demo_contact_repo_test.go`（SQLite 本地跑，CI 对三种库真跑）、`pkg/extdb/*_test.go` |

## 何时用

- **用**：数据的主人是客户的库（ERP、CRM、MES……），本系统只是读它，或者按约定改它。
- **不用**：本系统自己的表。主库固定 PostgreSQL，走 ent，按 [backend.md](./backend.md) 写。
- 外部库支持 **PostgreSQL、MySQL、SQL Server**。外部库不走 ent（ent 不支持 SQL Server），用 `database/sql` + 驱动，SQL 由 [goqu v9](https://github.com/doug-martin/goqu) 按方言构造，全部是占位符参数。
- **系统绝不对外部库建表或改表。** ent 自动迁移和版本化升级步骤只作用于主库。表结构由客户维护；Demo 的建表脚本要人手动执行。

## 配置

在 `configs/data.yaml` 的 `data.external_sources` 下登记，键是数据源名，代码按名字取：

```yaml
data:
  external_sources:
    erp:
      driver: sqlserver            # postgres | mysql | sqlserver
      dsn: "${GWA_EXT_ERP_DSN:}"   # 连接串放 .env，不进仓库
      read_only: true              # 不写默认 true；true 时写操作直接拒绝
      max_open_conns: 10           # 不写 10
      max_idle_conns: 2            # 不写 2
      conn_max_lifetime: 1800s     # 不写 1800s
      query_timeout: 5s            # 不写 5s；每条语句都套这个超时
      required: false              # true 才进入 /readyz
      timezone: "Asia/Shanghai"    # 外部库无时区时间字段按此解释；不写为 UTC
```

- 时长只能用秒写（`1800s`、`0.5s`），`30m` 这种写法配置加载会报错。
- 配置写错（driver 不认识、时区不认识、MySQL DSN 解析不了）启动失败，错误里只有数据源名，不带连接串。
- 外部库连不上**不影响启动**，只记一条错误日志；请求时返回 `EXTERNAL_SOURCE_UNAVAILABLE`。`required: true` 的源进 `/readyz`，失败时响应体是 `external:<名字> unavailable`。
- 各驱动的 DSN 写法：PostgreSQL `postgres://user:pass@host:5432/db?sslmode=disable`（pgx）；MySQL `user:pass@tcp(host:3306)/db`（go-sql-driver，`parseTime` 和 `loc` 由系统强制设为 `true` / `UTC`，自己写了也会被覆盖）；SQL Server `sqlserver://user:pass@host:1433?database=db`（go-mssqldb）。
- 配置定义在 `api/protos/conf/v1/kratos_conf_data.proto` 的 `Data.ExternalSource`。

Demo 用的数据源名固定是 `demo`。没配 `demo` 时页面的每个接口都返回 503 `EXTERNAL_SOURCE_NOT_CONFIGURED`（前端显示「外部数据源未配置」），不会 500。

## 只读与可写

外部库**通常只读，但必须支持可写**。

| 层 | 只读源（`read_only: true`，默认） | 可写源 |
|---|---|---|
| `extdb` | `Exec`、`Tx` 直接返回 403 `EXTERNAL_SOURCE_READ_ONLY`，不碰数据库 | 正常执行 |
| PG / MySQL | 查询在 `READ ONLY` 事务里执行，第二层保护：即使写语句混进查询也会被数据库拒绝，同样归为 `EXTERNAL_SOURCE_READ_ONLY` | — |
| SQL Server | 没有只读事务，**要求客户给只读账号**（只授 `SELECT`）。账号无写权限时数据库报 229，写操作归为 `EXTERNAL_SOURCE_READ_ONLY` | — |

部署时无论配置是不是只读，都建议向客户要最小权限账号：只读模块要只读账号，可写模块只授对应表的增删改查。

## 写一个外部数据模块

步骤和 ent 模块一样分层，区别在 repo 不用 ent。以 Demo 为例。

### 1. proto

照 [backend.md](./backend.md) Step 1–3 写领域 proto 和 BFF proto，有这些不同：

- **外部资源的 id 在 proto 里一律用 `string`**：外部主键是什么类型由客户决定（整数、UUID、编码）。Demo 的 id 由应用生成 UUID，列类型 `VARCHAR(36)` 三种库通用。
- **金额等定点数用 `string`**，不用 `double`。读写都是十进制字符串原样过，不经过 float。
- 没有 `created_by` 这类审计字段，也不要 `tenant_id`：外部表里有什么列，proto 就映射什么列。
- 请求体仍按本仓 CRUD 规范：`Create` / `Update` 包 `{ data: {...} }`，`Update` 带 `update_mask`，`List` 收 `pagination.PagingRequest`。Demo 的删除是单条 `DELETE /{id}`。
- 错误 reason 复用 `external.service.v1.ExternalErrorReason`，不用再写新的错误 proto。
- 新领域目录要在 `api/buf.gen.yaml` 加 `go_package` 覆盖（Demo 是 `external/service/v1` → `externalpb`）。之后 `make api && make openapi && make ts`。

### 2. repo

看 `external_demo_contact_repo.go`。要点：

1. **列白名单**：一个 `extdb.Table`，登记 API 字段名（snake_case）→ 外部列名和类型。只有登记过的字段能过滤和排序，其他一律 400。客户列名和 API 字段名不同时只改 `Column.Name`。
   ```go
   var demoContactTable = &extdb.Table{
       Name: "ext_demo_contacts",
       PK:   "id",
       Columns: map[string]extdb.Column{
           "id":        {Name: "id", Type: extdb.TypeString},
           "is_active": {Name: "is_active", Type: extdb.TypeBool},
           "amount":    {Name: "amount", Type: extdb.TypeDecimal},
           "created_at": {Name: "created_at", Type: extdb.TypeTime},
           // ...
       },
   }
   ```
2. **每个方法先 `sources.Get("demo")`**，没配置时它返回 `EXTERNAL_SOURCE_NOT_CONFIGURED`，直接往上返回。
3. **List**：`src.Page(src.From(table).Select(cols...), table, req)` 返回列表查询和计数查询，再各自 `src.Query`。
4. **Get / Create / Update / Delete**：用 `src.From`、`src.Insert`、`src.Update`、`src.Delete` 起头（都已参数化），`src.Query` 读、`src.Exec` 写。`Exec` 返回影响行数，0 行按 404 处理。
5. **Update 的 mask**：逐个 path 查「可更新字段」表，生成 `goqu.Record`。前端的 `updateMask` 末尾总带 `id`，忽略它；其他不在表里的 path 返回 400。mask 里有、值为空的字段写 NULL（前端清空金额就是这样）。
6. **扫描**：可空列用 `sql.NullString` / `sql.NullTime`；decimal 按字符串扫；布尔直接扫进 `bool`（SQL Server `bit`、MySQL `tinyint(1)` 都行）。
7. 不要自己拼 SQL 字符串，不要把请求里的字段名直接当列名。

### 3. service

看 `external_demo_contact_service.go`。和 ent 模块一样嵌入 `adminV1.<Xxx>ServiceHTTPServer`，入口校验 `req.Data == nil`、必填项、金额格式（Demo 按 `decimal(18,2)`，允许负数），错误用 `externalV1.ErrorBadRequest`。

### 4. 装配、菜单、权限

- `wiring_ent.go`：仓储构造时传 `externalSources`（注册表在「基础设施」一节建好，已经在那里了，不用再建）；服务、`NewRestServer` 实参、`rest_server.go` 路由照常加。`make register` 只会生成 ent 形态的构造行，要手改。
- 菜单：按 [backend.md](./backend.md) Step 11 在 `DefaultMenus` 加一行，并把 id 加进平台管理员权限的 `MenuIds`。Demo 是系统管理下的 id 71，路径 `/system/external-demo-contacts`。已部署的实例用「菜单同步」或手动加。
- 权限点：接口资源名取路径第一段转单数（`/admin/v1/external-demo-contacts` → `external-demo-contact:view|create|edit|delete`），先 `make openapi` 再重启，启动时自动同步。

### 5. 前端

（前端任务补充）

## 分页、过滤、排序

`src.Page` 把本仓的 `PagingRequest` 翻译成三种方言：

- 过滤条件来自 `query`（前端默认用它，如 `{"name__contains":"张"}`）、`filter`（AIP）或 `filterExpr`，字段名统一转 snake_case 后查白名单。
- 支持的操作符：`eq`/`exact`、`not`、`gt`/`gte`/`lt`/`lte`、`in`/`not_in`、`range`/`between`、`isnull`/`not_isnull`、`contains`/`icontains`、`startswith`/`istartswith`、`endswith`/`iendswith`、`iexact`。其他操作符（regex、json、日期部件……）返回 400。
- **contains 一律不区分大小写**：PG 用 `ILIKE`；MySQL、SQL Server 用 `LIKE`，大小写跟列的排序规则（常见的 `_ci` 不区分）。用户输入里的 `%`、`_` 按字面匹配：PG / MySQL 用反斜杠转义，SQL Server 用方括号。contains 只能用在字符串列上；搜索词为空时不加条件。
- 值按列类型转换，格式不对返回 400：布尔接受 `true/false/1/0`；整数；decimal 必须是十进制字符串（不接受科学计数法）；时间接受 RFC3339，或 `2006-01-02 15:04:05` / `2006-01-02`（按数据源时区）。
- 排序字段同样过白名单；**分页总有确定的排序**：没给排序按主键升序，给了但没含主键时末尾追加主键。
- 页大小默认 10，上限 100（超了截断）；`no_paging=true` 最多返回 1000 行。SQL Server 分页是 `OFFSET … FETCH`。

## 类型约定

| 外部列 | Go 扫描 | proto | 说明 |
|---|---|---|---|
| 主键 | `string` | `string` | 整数主键也转成 string 给前端 |
| `DECIMAL` / `NUMERIC` | `sql.NullString` | `string` | 不转 float |
| `BOOLEAN` / `BIT` / `TINYINT(1)` | `bool` | `bool` | 过滤值用 `true`/`false` |
| 无时区时间：PG `TIMESTAMP`、MySQL `DATETIME`、SQL Server `DATETIME2` | `sql.NullTime` → `src.TimeFromDB` | `Timestamp` | 写入前 `src.TimeToDB` |
| 带时区时间：PG `TIMESTAMPTZ`、SQL Server `DATETIMEOFFSET` | `sql.NullTime` | `Timestamp` | 不调 `TimeToDB` / `TimeFromDB`，原值读写 |

无时区时间列里存的是 `timezone` 时区的墙上时间。`TimeToDB` 把时间换算到该时区再交给驱动，`TimeFromDB` 把读到的墙上时间按该时区解释。MySQL 的 `TIMESTAMP` 列会按会话时区换算，接这种列时让客户确认会话时区。

## 事务边界

- 同一外部源内：`src.Tx(ctx, func(tx *extdb.Tx) error {...})`，返回 nil 提交，返回错误或 panic 回滚。只读源上 `Tx` 直接拒绝。事务里的每条语句各自套 `query_timeout`。
- **不支持跨库事务**：主库和外部库、两个外部库之间都不在一个事务里。要先写主库再写外部库（或反过来）时，自己设计补偿或幂等重试，不要假设能一起回滚。

## 客户改表结构时

系统不做结构同步，表结构变化靠改代码：

- 列改名：只改白名单里的 `Column.Name`、SELECT 列和 Insert/Update 的列名，API 字段名不动，前端不用改。
- 加列：要展示就按「改一个字段」的顺序加 proto 字段 → 白名单 → SELECT / 扫描 → Create / Update → 前端；不展示就不用管。
- 删列或改类型：对应 SQL 会在运行时报错（500，日志里有数据库原文）。上线前用客户的库跑一遍列表、详情、增改删。

## 数据权限与审计

- **数据权限范围不适用于外部数据**：角色上的「本部门 / 仅本人」等数据范围只过滤主库里挂了 `TenantAndDataScopePolicy` 的 ent 表。外部表没有本系统的组织和创建人概念，能看到某个外部模块的用户看到的是全部数据，只靠菜单 / 接口权限控制能不能进。确实要按行限制时，在 repo 里按业务列自己加条件，并在模块文档写明。
- **操作审计照常**：API 层的审计中间件按接口记录所有非 GET 请求，外部源的增改删也进操作日志。路径里的 UUID 会记成资源 ID（纯数字和 UUID 才识别，客户自定的编码形态的 ID 不进资源 ID 字段）。

## 错误

| reason | HTTP | 何时 |
|---|---|---|
| `EXTERNAL_SOURCE_NOT_CONFIGURED` | 503 | 配置里没有这个数据源 |
| `EXTERNAL_SOURCE_READ_ONLY` | 403 | 只读源上写，或数据库拒绝写权限 |
| `EXTERNAL_SOURCE_UNAVAILABLE` | 503 | 连不上、认证失败、库不存在 |
| `EXTERNAL_SOURCE_TIMEOUT` | 504 | 超过 `query_timeout` |
| `BAD_REQUEST` | 400 | 过滤/排序字段不在白名单、操作符不支持、值格式不对 |
| `NOT_FOUND` | 404 | 记录不存在 |
| `INTERNAL_SERVER_ERROR` | 500 | 其他数据库错误（约束冲突、列类型不符……） |

错误日志带数据源名和驱动原始错误；响应只带数据源名，不带连接串和数据库原文。前端两种语言的译文在 `_core/common.json` 的 `request.reason`。

## 性能注意

- **连接数**：每个外部源一个连接池，`max_open_conns` 默认 10。多实例部署时外部库看到的连接数是「实例数 × max_open_conns」，按客户库的承受能力定。
- **超时**：`query_timeout` 默认 5s，每条语句一个期限。超时返回 504，前端提示缩小查询范围。不要为了一个慢查询把全局超时调得很大，先让客户给过滤和排序用的列建索引。
- **分页上限**：页大小上限 100、`no_paging` 上限 1000，外部大表上不要做全量导出。
- **contains** 是 `LIKE '%…%'`，用不上普通索引，大表上要配合别的条件。
- SQL Server 上 Go 的字符串参数按 `NVARCHAR` 发送，和 `VARCHAR` 列比较会隐式转换、可能用不上索引；大表的 `VARCHAR` 主键 / 过滤列请客户评估是否改 `NVARCHAR`。

## 测试

- 底层包：`pkg/extdb/paging_test.go` 不连库，断言三种方言生成的 SQL；`source_sqlite_test.go` 在 SQLite 上跑读写、只读拒绝、事务、超时；`registry_test.go` 覆盖配置错误和连不上的源。
- 模块仓储：写一个 `runXxxSuite(t, reg)`，本地用 SQLite 建表跑，再在集成测试里对三种库各跑一遍（照 `external_demo_contact_repo_test.go`）。
- 集成测试读 `EXT_TEST_POSTGRES_DSN`、`EXT_TEST_MYSQL_DSN`、`EXT_TEST_SQLSERVER_DSN`，没设就 `t.Skip`，本地 `go test ./...` 不受影响。CI 的 `.github/workflows/external-source.yml` 起三种库真跑，有子测试被跳过就判失败。
