package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/menu"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

func nopUpgradeLogger() *bLogger.Helper { return bLogger.NewHelper(bLogger.NopLogger()) }

type upgradeRecord struct {
	Version int
	Name    string
}

func readUpgradeRecords(t *testing.T, drv *entsql.Driver) []upgradeRecord {
	t.Helper()
	var rows entsql.Rows
	require.NoError(t, drv.Query(context.Background(),
		"SELECT version, name FROM sys_schema_upgrades ORDER BY version", []any{}, &rows))
	defer rows.Close()
	var out []upgradeRecord
	for rows.Next() {
		var r upgradeRecord
		require.NoError(t, rows.Scan(&r.Version, &r.Name))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func sqliteTableExists(t *testing.T, drv *entsql.Driver, name string) bool {
	t.Helper()
	var rows entsql.Rows
	require.NoError(t, drv.Query(context.Background(),
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", []any{name}, &rows))
	defer rows.Close()
	require.True(t, rows.Next())
	var n int
	require.NoError(t, rows.Scan(&n))
	return n > 0
}

// TestRunSchemaUpgrades_FreshDB 全新库：全部步骤按版本号顺序执行并各记一行；再启动一次不新增记录。
func TestRunSchemaUpgrades_FreshDB(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	drv := entClient.Driver()
	ctx := context.Background()

	require.NoError(t, RunSchemaUpgrades(ctx, drv, nopUpgradeLogger()))
	want := make([]upgradeRecord, 0, len(schemaUpgradeSteps))
	for _, s := range schemaUpgradeSteps {
		want = append(want, upgradeRecord{Version: s.Version, Name: s.Name})
	}
	require.Equal(t, want, readUpgradeRecords(t, drv))

	require.NoError(t, RunSchemaUpgrades(ctx, drv, nopUpgradeLogger()))
	require.Equal(t, want, readUpgradeRecords(t, drv))
}

// TestRunSchemaUpgrades_SkipsApplied 执行过的步骤不再执行，新追加的步骤只执行新的那一步。
func TestRunSchemaUpgrades_SkipsApplied(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	drv := entClient.Driver()
	ctx := context.Background()

	var order []int
	step := func(v int) schemaUpgradeStep {
		return schemaUpgradeStep{Version: v, Name: "s", Run: func(context.Context, *ent.Client, dialect.ExecQuerier, *bLogger.Helper) error {
			order = append(order, v)
			return nil
		}}
	}

	require.NoError(t, runSchemaUpgrades(ctx, drv, []schemaUpgradeStep{step(1), step(2)}, nopUpgradeLogger()))
	require.Equal(t, []int{1, 2}, order)

	require.NoError(t, runSchemaUpgrades(ctx, drv, []schemaUpgradeStep{step(1), step(2)}, nopUpgradeLogger()))
	require.Equal(t, []int{1, 2}, order, "重复启动不应重复执行")

	require.NoError(t, runSchemaUpgrades(ctx, drv, []schemaUpgradeStep{step(1), step(2), step(5)}, nopUpgradeLogger()))
	require.Equal(t, []int{1, 2, 5}, order)
	require.Len(t, readUpgradeRecords(t, drv), 3)
}

// TestRunSchemaUpgrades_FailedStepRollsBack 某步失败：返回原始错误，该步的写入回滚、不写记录，后续步骤不执行；
// 修好后重启从失败的那一步继续。
func TestRunSchemaUpgrades_FailedStepRollsBack(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	drv := entClient.Driver()
	ctx := context.Background()
	sysCtx := enttest.NewSystemContext(ctx)

	boom := errors.New("boom")
	fail := true
	var ran []int
	steps := func() []schemaUpgradeStep {
		return []schemaUpgradeStep{
			{Version: 1, Name: "ok", Run: func(context.Context, *ent.Client, dialect.ExecQuerier, *bLogger.Helper) error {
				ran = append(ran, 1)
				return nil
			}},
			{Version: 2, Name: "writes_then_fails", Run: func(ctx context.Context, c *ent.Client, _ dialect.ExecQuerier, _ *bLogger.Helper) error {
				ran = append(ran, 2)
				// ent 写入走本步事务，失败时一起回滚
				c.Menu.Create().SetPath("/upgrade-probe").SetName("Probe").SetType(menu.TypeCatalog).SaveX(ctx)
				if fail {
					return boom
				}
				return nil
			}},
			{Version: 3, Name: "after", Run: func(context.Context, *ent.Client, dialect.ExecQuerier, *bLogger.Helper) error {
				ran = append(ran, 3)
				return nil
			}},
		}
	}

	err := runSchemaUpgrades(ctx, drv, steps(), nopUpgradeLogger())
	require.ErrorIs(t, err, boom)
	require.Contains(t, err.Error(), "step 2 writes_then_fails")
	require.Equal(t, []int{1, 2}, ran, "失败后不再执行后续步骤")
	require.Equal(t, []upgradeRecord{{Version: 1, Name: "ok"}}, readUpgradeRecords(t, drv), "失败的步骤不写记录")
	require.Zero(t, entClient.Client().Menu.Query().Where(menu.PathEQ("/upgrade-probe")).CountX(sysCtx), "失败步骤的写入应回滚")

	fail = false
	ran = nil
	require.NoError(t, runSchemaUpgrades(ctx, drv, steps(), nopUpgradeLogger()))
	require.Equal(t, []int{2, 3}, ran)
	require.Len(t, readUpgradeRecords(t, drv), 3)
	require.Equal(t, 1, entClient.Client().Menu.Query().Where(menu.PathEQ("/upgrade-probe")).CountX(sysCtx))
}

// TestRunSchemaUpgrades_RejectsOutOfOrderSteps 版本号必须严格递增，写错时启动失败而不是悄悄跳过。
func TestRunSchemaUpgrades_RejectsOutOfOrderSteps(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	noop := func(context.Context, *ent.Client, dialect.ExecQuerier, *bLogger.Helper) error { return nil }
	err := runSchemaUpgrades(context.Background(), entClient.Driver(),
		[]schemaUpgradeStep{{Version: 2, Name: "b", Run: noop}, {Version: 1, Name: "a", Run: noop}}, nopUpgradeLogger())
	require.ErrorContains(t, err, "out of order")
}

// TestRunSchemaUpgrades_LegacyDB 旧库：遗留租户配置被清掉，sys_tenants / sys_plans* 四张表被删掉（含外键）。
func TestRunSchemaUpgrades_LegacyDB(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	drv := entClient.Driver()
	client := entClient.Client()
	ctx := context.Background()
	sysCtx := enttest.NewSystemContext(ctx)

	// 旧版 ent 生成的表结构（只保留主键和外键列），外键方向同旧 schema
	for _, ddl := range []string{
		`CREATE TABLE sys_plans (id integer PRIMARY KEY AUTOINCREMENT, name text)`,
		`CREATE TABLE sys_plan_modules (id integer PRIMARY KEY AUTOINCREMENT, plan_id integer REFERENCES sys_plans(id))`,
		`CREATE TABLE sys_plan_quotas (id integer PRIMARY KEY AUTOINCREMENT, plan_id integer REFERENCES sys_plans(id))`,
		`CREATE TABLE sys_tenants (id integer PRIMARY KEY AUTOINCREMENT, plan_id integer REFERENCES sys_plans(id))`,
		`INSERT INTO sys_plans (id, name) VALUES (1, 'basic')`,
		`INSERT INTO sys_plan_modules (plan_id) VALUES (1)`,
		`INSERT INTO sys_plan_quotas (plan_id) VALUES (1)`,
		`INSERT INTO sys_tenants (plan_id) VALUES (1)`,
	} {
		require.NoError(t, drv.Exec(ctx, ddl, []any{}, nil), ddl)
	}
	db := seedLegacyTenantDB(t, client, sysCtx)

	require.NoError(t, RunSchemaUpgrades(ctx, drv, nopUpgradeLogger()))

	for _, table := range legacyTenantTables {
		require.False(t, sqliteTableExists(t, drv, table), table)
	}
	require.Zero(t, client.Menu.Query().Where(menu.IDIn(db.tenantMenuIDs...)).CountX(sysCtx))
	require.Equal(t, sortedIDs(db.keepPermIDs), sortedIDs(client.Permission.Query().IDsX(sysCtx)))
	require.True(t, sqliteTableExists(t, drv, "sys_menus"))
}

// TestSchemaUpgradeRecordInsert 记录行 INSERT 的方言构造：PostgreSQL（含 pgx 驱动名）用 $n，MySQL / SQLite 用 ?。
func TestSchemaUpgradeRecordInsert(t *testing.T) {
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	step := schemaUpgradeStep{Version: 7, Name: "x"}

	for _, drv := range []string{dialect.Postgres, "pgx"} {
		q, args := schemaUpgradeRecordInsert(drv, step, at)
		require.Equal(t, `INSERT INTO "sys_schema_upgrades" ("version", "name", "applied_at") VALUES ($1, $2, $3)`, q, drv)
		require.Equal(t, []any{7, "x", at}, args)
	}
	q, _ := schemaUpgradeRecordInsert(dialect.MySQL, step, at)
	require.Equal(t, "INSERT INTO `sys_schema_upgrades` (`version`, `name`, `applied_at`) VALUES (?, ?, ?)", q)
	q, _ = schemaUpgradeRecordInsert(dialect.SQLite, step, at)
	require.Equal(t, "INSERT INTO `sys_schema_upgrades` (`version`, `name`, `applied_at`) VALUES (?, ?, ?)", q)
}
