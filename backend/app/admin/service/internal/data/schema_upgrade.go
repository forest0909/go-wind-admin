package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/third_party/go-crud/viewer"
)

// 版本化的数据库升级步骤。
//
// ent 自动迁移（Schema.Create）只加表、加列、加索引；删表删列、数据回填、清理旧数据这类
// 一次性的事写成这里的步骤。启动时在 Schema.Create 之后、默认数据播种之前，按版本号
// 顺序执行还没执行过的步骤，每步成功后在 sys_schema_upgrades 记一行，以后不再执行。
// 多实例互斥靠外层的启动锁（AcquireStartupLock）。
//
// 写步骤的约定（见 docs/field_change_guide.md「升级步骤」）：
//   - 只往列表末尾追加，版本号递增，已发布的步骤不改不删；
//   - 全新库也会执行每一步，所以要对空库安全（DROP TABLE IF EXISTS、按条件删）；
//   - 每步在一个事务里执行，记录行和步骤同一事务提交。MySQL 的 DDL 会隐式提交，
//     含 DDL 的步骤在 MySQL 上做不到原子，失败后重启会整步重跑，所以步骤要能重复执行。

// schemaUpgradeTable 升级记录表。不放进 ent schema：它只给本文件用，不需要生成代码。
const schemaUpgradeTable = "sys_schema_upgrades"

// createSchemaUpgradeTableSQL PostgreSQL / MySQL / SQLite 通用。
const createSchemaUpgradeTableSQL = `CREATE TABLE IF NOT EXISTS sys_schema_upgrades (
    version INTEGER NOT NULL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    applied_at TIMESTAMP NOT NULL
)`

// schemaUpgradeStep 一个升级步骤。client 绑定在本步的事务上；exec 用来执行原生 SQL（同一事务）。
type schemaUpgradeStep struct {
	Version int
	Name    string
	Run     func(ctx context.Context, client *ent.Client, exec dialect.ExecQuerier, l *bLogger.Helper) error
}

// schemaUpgradeSteps 全部升级步骤，按版本号升序，只追加。
var schemaUpgradeSteps = []schemaUpgradeStep{
	{Version: 1, Name: "purge_legacy_tenant_config", Run: upgradePurgeLegacyTenantConfig},
	{Version: 2, Name: "drop_legacy_tenant_tables", Run: upgradeDropLegacyTenantTables},
}

// RunSchemaUpgrades 执行还没执行过的升级步骤。任何一步失败立即返回原始错误，该步不写记录。
func RunSchemaUpgrades(ctx context.Context, drv *entsql.Driver, l *bLogger.Helper) error {
	return runSchemaUpgrades(ctx, drv, schemaUpgradeSteps, l)
}

func runSchemaUpgrades(ctx context.Context, drv *entsql.Driver, steps []schemaUpgradeStep, l *bLogger.Helper) error {
	ctx = viewer.WithSystemContext(ctx)

	if err := drv.Exec(ctx, createSchemaUpgradeTableSQL, []any{}, nil); err != nil {
		return fmt.Errorf("create %s: %w", schemaUpgradeTable, err)
	}
	applied, err := appliedSchemaUpgrades(ctx, drv)
	if err != nil {
		return fmt.Errorf("read %s: %w", schemaUpgradeTable, err)
	}

	last := 0
	for _, step := range steps {
		if step.Version <= last {
			return fmt.Errorf("schema upgrade steps out of order: version %d after %d", step.Version, last)
		}
		last = step.Version
		if applied[step.Version] {
			continue
		}
		l.Infof(ctx, "[UPGRADE] running step %d %s", step.Version, step.Name)
		if err = runSchemaUpgradeStep(ctx, drv, step, l); err != nil {
			return fmt.Errorf("schema upgrade step %d %s: %w", step.Version, step.Name, err)
		}
		l.Infof(ctx, "[UPGRADE] step %d %s done", step.Version, step.Name)
	}
	return nil
}

func appliedSchemaUpgrades(ctx context.Context, drv *entsql.Driver) (map[int]bool, error) {
	var rows entsql.Rows
	if err := drv.Query(ctx, "SELECT version FROM "+schemaUpgradeTable, []any{}, &rows); err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func runSchemaUpgradeStep(ctx context.Context, drv *entsql.Driver, step schemaUpgradeStep, l *bLogger.Helper) (err error) {
	tx, err := drv.Tx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			l.Errorf(ctx, "[UPGRADE] rollback step %d failed: %v", step.Version, rbErr)
		}
	}()

	client := ent.NewClient(ent.Driver(upgradeTxDriver{ExecQuerier: tx, name: drv.Dialect()}))
	if err = step.Run(ctx, client, tx, l); err != nil {
		return err
	}

	query, args := schemaUpgradeRecordInsert(drv.Dialect(), step, time.Now().UTC())
	if err = tx.Exec(ctx, query, args, nil); err != nil {
		return fmt.Errorf("record step: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// schemaUpgradeRecordInsert 按方言构造记录行的 INSERT（PostgreSQL 用 $n 占位符）。
func schemaUpgradeRecordInsert(driverName string, step schemaUpgradeStep, at time.Time) (string, []any) {
	if isPostgresDriver(driverName) {
		driverName = dialect.Postgres
	}
	return entsql.Dialect(driverName).
		Insert(schemaUpgradeTable).
		Columns("version", "name", "applied_at").
		Values(step.Version, step.Name, at).
		Query()
}

// upgradeTxDriver 把一个已开启的事务包成 ent 驱动，让步骤里的 ent 读写都落在这个事务上。
type upgradeTxDriver struct {
	dialect.ExecQuerier
	name string
}

func (d upgradeTxDriver) Tx(context.Context) (dialect.Tx, error) { return dialect.NopTx(d), nil }
func (d upgradeTxDriver) Close() error                           { return nil }
func (d upgradeTxDriver) Dialect() string                        { return d.name }

// ───────────────────────── 步骤 ─────────────────────────

// upgradePurgeLegacyTenantConfig v1：删除旧库遗留的租户 / 套餐菜单、权限点、模板角色和权限组。
// 必须在启动期权限点同步之前，否则同步会从遗留菜单再派生出权限点——升级步骤在播种和同步之前执行。
func upgradePurgeLegacyTenantConfig(ctx context.Context, client *ent.Client, _ dialect.ExecQuerier, l *bLogger.Helper) error {
	res, err := purgeLegacyTenantConfig(ctx, client)
	if err != nil {
		return err
	}
	if !res.Empty() {
		l.Infof(ctx, "[UPGRADE] 已清理遗留租户配置: 菜单 %d、权限点 %d、角色 %d、权限组 %d",
			res.Menus, res.Permissions, res.Roles, res.PermissionGroups)
	}
	return nil
}

// legacyTenantTables 去租户后不再使用的表，按外键依赖自子向父排列。
// ent schema 里已没有对应实体，代码里没有读写。
var legacyTenantTables = []string{"sys_plan_modules", "sys_plan_quotas", "sys_tenants", "sys_plans"}

// upgradeDropLegacyTenantTables v2：删除租户 / 套餐遗留表。
func upgradeDropLegacyTenantTables(ctx context.Context, _ *ent.Client, exec dialect.ExecQuerier, l *bLogger.Helper) error {
	l.Infof(ctx, "[UPGRADE] dropping legacy tenant tables if exist: %v", legacyTenantTables)
	for _, table := range legacyTenantTables {
		if err := exec.Exec(ctx, "DROP TABLE IF EXISTS "+table, []any{}, nil); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	return nil
}
