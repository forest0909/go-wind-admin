package ent_test

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	ent "go-wind-admin/app/admin/service/internal/data/ent"
	_ "go-wind-admin/app/admin/service/internal/data/ent/runtime"
)

// openGuardClient 打开守卫测试用的 ent client。
//
// 默认用 SQLite 内存库（每次调用独占一个库），go test ./... 无需任何外部依赖就会真正执行
// 租户/数据范围守卫断言——此前默认连本地 PG、连不上就 Skip，套件常绿却从未验证过隔离。
// 设置 GUARD_TEST_PG_DSN 时改用该 PostgreSQL 代替 SQLite 跑（不是两种都跑；连不上直接失败，不再静默跳过），
// 并 TRUNCATE 给定表复位。
func openGuardClient(t *testing.T, truncateTables ...string) *ent.Client {
	t.Helper()

	var drv *sql.Driver
	if dsn := os.Getenv("GUARD_TEST_PG_DSN"); dsn != "" {
		var err error
		drv, err = sql.Open(dialect.Postgres, dsn)
		if err != nil {
			t.Fatalf("open postgres: %v", err)
		}
		if err := drv.DB().Ping(); err != nil {
			_ = drv.Close()
			t.Fatalf("GUARD_TEST_PG_DSN 已设置但连接失败: %v", err)
		}
	} else {
		db, err := stdsql.Open("sqlite", fmt.Sprintf(
			"file:guard_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", time.Now().UnixNano()))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		drv = sql.OpenDB(dialect.SQLite, db)
	}

	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if drv.Dialect() == dialect.Postgres && len(truncateTables) > 0 {
		if _, err := drv.DB().Exec("TRUNCATE " + strings.Join(truncateTables, ", ") + " CASCADE"); err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}
	return client
}
