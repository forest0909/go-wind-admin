package data

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "modernc.org/sqlite"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
	"go-wind-admin/pkg/extdb"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// 外部数据示例的仓储测试：同一套用例在本地跑 SQLite，在 CI 里对 PG / MySQL / SQL Server 真跑
// （设置了 EXT_TEST_{POSTGRES,MYSQL,SQLSERVER}_DSN 才跑，见 .github/workflows/external-source.yml）。

func newDemoContactRepoForTest(reg *extdb.Registry) *ExternalDemoContactRepo {
	return &ExternalDemoContactRepo{log: bLogger.NewHelper(bLogger.NopLogger()), sources: reg}
}

func strPtr(s string) *string { return &s }

// runDemoContactSuite 在空的 ext_demo_contacts 表上跑完整 CRUD + 分页/过滤/排序/contains。
func runDemoContactSuite(t *testing.T, reg *extdb.Registry) {
	ctx := context.Background()
	repo := newDemoContactRepoForTest(reg)
	src, err := reg.Get(externalDemoSource)
	require.NoError(t, err)

	// Create
	ids := map[string]string{}
	for _, c := range []*externalV1.DemoContact{
		{Name: strPtr("Alice Zhang"), Code: strPtr("C001"), Amount: strPtr("1234.50"), Remark: strPtr("VIP")},
		{Name: strPtr("bob li"), Code: strPtr("C002"), Amount: strPtr("0.10"), IsActive: new(bool)},
		{Name: strPtr("100%_off"), Code: strPtr("C003")},
	} {
		id, err := repo.Create(ctx, c)
		require.NoError(t, err)
		require.Len(t, id, 36)
		ids[c.GetName()] = id
	}

	// Get：类型约定（布尔、decimal 字符串原样、时间按时区）
	before := time.Now().Add(-time.Minute)
	alice, err := repo.Get(ctx, ids["Alice Zhang"])
	require.NoError(t, err)
	require.Equal(t, "Alice Zhang", alice.GetName())
	require.True(t, alice.GetIsActive(), "is_active defaults to true")
	require.Equal(t, "1234.50", alice.GetAmount())
	require.Equal(t, "VIP", alice.GetRemark())
	require.WithinDuration(t, time.Now(), alice.GetCreatedAt().AsTime(), 2*time.Minute)
	require.True(t, alice.GetCreatedAt().AsTime().After(before))

	bob, err := repo.Get(ctx, ids["bob li"])
	require.NoError(t, err)
	require.False(t, bob.GetIsActive())
	require.Equal(t, "0.10", bob.GetAmount())
	require.Nil(t, bob.Remark, "NULL stays nil")

	_, err = repo.Get(ctx, "00000000-0000-0000-0000-000000000000")
	require.True(t, externalV1.IsNotFound(err), "got %v", err)

	// List：contains 不区分大小写，通配符按字面
	list := func(req *paginationV1.PagingRequest) ([]string, uint64) {
		t.Helper()
		resp, err := repo.List(ctx, req)
		require.NoError(t, err)
		var names []string
		for _, it := range resp.GetItems() {
			names = append(names, it.GetName())
		}
		return names, resp.GetTotal()
	}
	q := func(query, orderBy string) *paginationV1.PagingRequest {
		r := &paginationV1.PagingRequest{FilteringType: &paginationV1.PagingRequest_Query{Query: query}}
		if orderBy != "" {
			r.OrderBy = &orderBy
		}
		return r
	}

	names, total := list(q(`{"name__contains":"ALICE"}`, ""))
	require.Equal(t, []string{"Alice Zhang"}, names)
	require.EqualValues(t, 1, total)

	names, _ = list(q(`{"name__contains":"%_"}`, ""))
	require.Equal(t, []string{"100%_off"}, names)

	names, _ = list(q(`{"is_active":"false"}`, ""))
	require.Equal(t, []string{"bob li"}, names)

	names, _ = list(q(`{"amount__gte":"1"}`, ""))
	require.Equal(t, []string{"Alice Zhang"}, names)

	names, _ = list(q(`{"code__in":"[\"C001\",\"C003\"]"}`, `["-code"]`))
	require.Equal(t, []string{"100%_off", "Alice Zhang"}, names)

	// 分页：第 2 页每页 2 条，按编码升序
	page, size := uint32(2), uint32(2)
	req := q(`{}`, `["code"]`)
	req.Page, req.PageSize = &page, &size
	names, total = list(req)
	require.Equal(t, []string{"100%_off"}, names)
	require.EqualValues(t, 3, total)

	// 白名单外字段 400
	_, err = repo.List(ctx, q(`{"secret__contains":"x"}`, ""))
	require.True(t, externalV1.IsBadRequest(err), "got %v", err)

	// Update：只改 mask 里的字段；mask 里的空值写 NULL
	time.Sleep(10 * time.Millisecond)
	err = repo.Update(ctx, ids["Alice Zhang"], &externalV1.DemoContact{
		Name: strPtr("Alice Wang"), Amount: strPtr("99.99"), Code: strPtr("IGNORED"),
	}, &fieldmaskpb.FieldMask{Paths: []string{"name", "amount", "remark", "isActive"}})
	require.NoError(t, err)
	alice2, err := repo.Get(ctx, ids["Alice Zhang"])
	require.NoError(t, err)
	require.Equal(t, "Alice Wang", alice2.GetName())
	require.Equal(t, "99.99", alice2.GetAmount())
	require.Equal(t, "C001", alice2.GetCode(), "code is outside the mask")
	require.Nil(t, alice2.Remark, "remark in mask with no value is cleared")
	require.False(t, alice2.GetIsActive())
	require.True(t, alice2.GetUpdatedAt().AsTime().After(alice.GetUpdatedAt().AsTime()))
	require.Equal(t, alice.GetCreatedAt().AsTime(), alice2.GetCreatedAt().AsTime())

	err = repo.Update(ctx, ids["Alice Zhang"], &externalV1.DemoContact{}, &fieldmaskpb.FieldMask{Paths: []string{"id"}})
	require.True(t, externalV1.IsBadRequest(err), "id is not updatable: %v", err)
	err = repo.Update(ctx, ids["Alice Zhang"], &externalV1.DemoContact{}, nil)
	require.True(t, externalV1.IsBadRequest(err), "mask required: %v", err)
	err = repo.Update(ctx, "00000000-0000-0000-0000-000000000000", &externalV1.DemoContact{Name: strPtr("x")},
		&fieldmaskpb.FieldMask{Paths: []string{"name"}})
	require.True(t, externalV1.IsNotFound(err), "got %v", err)

	// Delete
	require.NoError(t, repo.Delete(ctx, ids["bob li"]))
	err = repo.Delete(ctx, ids["bob li"])
	require.True(t, externalV1.IsNotFound(err), "got %v", err)
	_, total = list(q(`{}`, ""))
	require.EqualValues(t, 2, total)

	// 时间按配置时区：库里存的是上海墙上时间
	var stored time.Time
	require.NoError(t, src.Query(ctx,
		src.From(demoContactTable.Name).Select("created_at").Where(goqu.Ex{"id": ids["100%_off"]}),
		func(rows *sql.Rows) error { return rows.Scan(&stored) }))
	created, err := repo.Get(ctx, ids["100%_off"])
	require.NoError(t, err)
	inZone := created.GetCreatedAt().AsTime().In(src.Location())
	require.Equal(t, inZone.Hour(), stored.Hour(), "naive column holds wall clock in the configured zone")
}

// runDemoContactReadOnly 只读源：读照常，写全部拒绝，表里不多不少。
func runDemoContactReadOnly(t *testing.T, reg *extdb.Registry) {
	ctx := context.Background()
	repo := newDemoContactRepoForTest(reg)

	before, err := repo.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)

	_, err = repo.Create(ctx, &externalV1.DemoContact{Name: strPtr("x")})
	require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)
	err = repo.Update(ctx, "any", &externalV1.DemoContact{Name: strPtr("x")}, &fieldmaskpb.FieldMask{Paths: []string{"name"}})
	require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)
	err = repo.Delete(ctx, "any")
	require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)

	after, err := repo.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Equal(t, before.GetTotal(), after.GetTotal())
}

func TestExternalDemoContactRepo_NotConfigured(t *testing.T) {
	ctx := context.Background()
	repo := newDemoContactRepoForTest(extdb.NewStaticRegistry())

	_, err := repo.List(ctx, &paginationV1.PagingRequest{})
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)
	_, err = repo.Get(ctx, "x")
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)
	_, err = repo.Create(ctx, &externalV1.DemoContact{Name: strPtr("x")})
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)
	err = repo.Update(ctx, "x", &externalV1.DemoContact{}, &fieldmaskpb.FieldMask{Paths: []string{"name"}})
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)
	err = repo.Delete(ctx, "x")
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)
}

// sqliteDemoDDL SQLite 版建表（只给单元测试；amount 用 TEXT，避免 SQLite 的数值亲和把 "0.10" 变成 0.1）
const sqliteDemoDDL = `CREATE TABLE ext_demo_contacts (
  id VARCHAR(36) NOT NULL PRIMARY KEY, name VARCHAR(100) NOT NULL, code VARCHAR(50),
  is_active BOOLEAN NOT NULL DEFAULT 1, amount TEXT, remark VARCHAR(500),
  created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`

func sqliteDemoRegistry(t *testing.T, readOnly bool) *extdb.Registry {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(sqliteDemoDDL)
	require.NoError(t, err)
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	src, err := extdb.NewSource(externalDemoSource, "sqlite3", db, extdb.Options{ReadOnly: readOnly, Location: loc})
	require.NoError(t, err)
	return extdb.NewStaticRegistry(src)
}

func TestExternalDemoContactRepo_SQLite(t *testing.T) {
	runDemoContactSuite(t, sqliteDemoRegistry(t, false))
}

func TestExternalDemoContactRepo_SQLiteReadOnly(t *testing.T) {
	runDemoContactReadOnly(t, sqliteDemoRegistry(t, true))
}

// TestExternalDemoContactRepo_Integration 三种外部库真跑：用 backend/sql/external_demo/<driver>.sql 建表，
// 走 NewRegistry（和生产同一条配置路径），跑完整用例和只读用例。DSN 环境变量没设就跳过。
func TestExternalDemoContactRepo_Integration(t *testing.T) {
	for _, driver := range []string{extdb.DriverPostgres, extdb.DriverMySQL, extdb.DriverSQLServer} {
		t.Run(driver, func(t *testing.T) {
			dsn := os.Getenv("EXT_TEST_" + strings.ToUpper(driver) + "_DSN")
			if dsn == "" {
				t.Skipf("EXT_TEST_%s_DSN not set", strings.ToUpper(driver))
			}
			reg := integrationRegistry(t, driver, dsn, false)
			src, err := reg.Get(externalDemoSource)
			require.NoError(t, err)

			// 测试自己建表、删表；系统代码从不建表。
			ddl, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "sql", "external_demo", driver+".sql"))
			require.NoError(t, err)
			execRaw(t, src, `DROP TABLE IF EXISTS ext_demo_contacts`)
			execRaw(t, src, string(ddl))
			t.Cleanup(func() { execRaw(t, src, `DROP TABLE IF EXISTS ext_demo_contacts`) })

			runDemoContactSuite(t, reg)
			runDemoContactReadOnly(t, integrationRegistry(t, driver, dsn, true))
		})
	}
}

func integrationRegistry(t *testing.T, driver, dsn string, readOnly bool) *extdb.Registry {
	t.Helper()
	reg, cleanup, err := extdb.NewRegistry(context.Background(), bLogger.NewHelper(bLogger.NopLogger()),
		map[string]*conf.Data_ExternalSource{externalDemoSource: {
			Driver:       driver,
			Dsn:          dsn,
			ReadOnly:     &readOnly,
			QueryTimeout: durationpb.New(10 * time.Second),
			Timezone:     "Asia/Shanghai",
		}})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	return reg
}

type rawStmt string

func (r rawStmt) ToSQL() (string, []any, error) { return string(r), nil, nil }

func execRaw(t *testing.T, src *extdb.Source, stmt string) {
	t.Helper()
	_, err := src.Exec(context.Background(), rawStmt(stmt))
	require.NoError(t, err)
}
