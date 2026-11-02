package extdb

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

func nopLog() *bLogger.Helper { return bLogger.NewHelper(bLogger.NopLogger()) }

func TestNewRegistry_ConfigErrorsFailStartup(t *testing.T) {
	cases := map[string]*conf.Data_ExternalSource{
		"unknown driver": {Driver: "oracle", Dsn: "x"},
		"bad timezone":   {Driver: DriverPostgres, Dsn: "host=127.0.0.1", Timezone: "Mars/Olympus"},
		"bad mysql dsn":  {Driver: DriverMySQL, Dsn: "user:secret@tcp(127.0.0.1:3306"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := NewRegistry(context.Background(), nopLog(), map[string]*conf.Data_ExternalSource{"erp": c})
			require.Error(t, err)
			require.Contains(t, err.Error(), `"erp"`)
			require.NotContains(t, err.Error(), "secret", "dsn must not leak")
		})
	}
}

// 连不上的源不影响启动：注册表照建，请求时返回 EXTERNAL_SOURCE_UNAVAILABLE；required 的进 /readyz。
func TestNewRegistry_UnreachableSource(t *testing.T) {
	reg, cleanup, err := NewRegistry(context.Background(), nopLog(), map[string]*conf.Data_ExternalSource{
		"erp": {
			Driver:       DriverPostgres,
			Dsn:          "postgres://u:secret@127.0.0.1:1/db?sslmode=disable&connect_timeout=2",
			Required:     true,
			QueryTimeout: durationpb.New(3 * time.Second),
		},
		"crm": {Driver: DriverMySQL, Dsn: "u:secret@tcp(127.0.0.1:1)/db?timeout=2s"},
	})
	require.NoError(t, err)
	defer cleanup()

	erp, err := reg.Get("erp")
	require.NoError(t, err)
	require.True(t, erp.ReadOnly(), "read_only defaults to true")

	err = erp.Query(context.Background(), erp.From("t").Select("id"), func(*sql.Rows) error { return nil })
	require.True(t, externalV1.IsExternalSourceUnavailable(err), "got %v", err)
	require.NotContains(t, err.Error(), "secret")

	crm, err := reg.Get("crm")
	require.NoError(t, err)
	err = crm.Query(context.Background(), crm.From("t").Select("id"), func(*sql.Rows) error { return nil })
	require.True(t, externalV1.IsExternalSourceUnavailable(err), "got %v", err)

	required := reg.Required()
	require.Len(t, required, 1)
	require.Equal(t, "erp", required[0].Name())
	require.Error(t, required[0].Ping(context.Background()))
}

func TestNewRegistry_ExplicitWritable(t *testing.T) {
	no := false
	reg, cleanup, err := NewRegistry(context.Background(), nopLog(), map[string]*conf.Data_ExternalSource{
		"erp": {Driver: DriverSQLServer, Dsn: "sqlserver://u:p@127.0.0.1:1?database=db&dial+timeout=1", ReadOnly: &no},
	})
	require.NoError(t, err)
	defer cleanup()
	s, err := reg.Get("erp")
	require.NoError(t, err)
	require.False(t, s.ReadOnly())
	require.Equal(t, DefaultQueryTimeout, s.timeout)
	require.Equal(t, time.UTC, s.Location())
}

// TestIntegration_ReadOnlyAndTimeout 三种外部库真跑（CI 设 EXT_TEST_<DRIVER>_DSN）：
// 只读源的写语句即使绕过 Exec 混进查询，PG/MySQL 也会被只读事务挡住；慢查询按 query_timeout 超时。
func TestIntegration_ReadOnlyAndTimeout(t *testing.T) {
	sleepSQL := map[string]string{
		DriverPostgres:  `SELECT pg_sleep(3)`,
		DriverMySQL:     `SELECT SLEEP(3)`,
		DriverSQLServer: `WAITFOR DELAY '00:00:03'; SELECT 1`,
	}
	for _, driver := range []string{DriverPostgres, DriverMySQL, DriverSQLServer} {
		t.Run(driver, func(t *testing.T) {
			dsn := os.Getenv("EXT_TEST_" + strings.ToUpper(driver) + "_DSN")
			if dsn == "" {
				t.Skipf("EXT_TEST_%s_DSN not set", strings.ToUpper(driver))
			}
			ctx := context.Background()
			open := func(readOnly bool, timeout time.Duration) *Source {
				reg, cleanup, err := NewRegistry(ctx, nopLog(), map[string]*conf.Data_ExternalSource{"it": {
					Driver: driver, Dsn: dsn, ReadOnly: &readOnly, QueryTimeout: durationpb.New(timeout),
				}})
				require.NoError(t, err)
				t.Cleanup(cleanup)
				s, err := reg.Get("it")
				require.NoError(t, err)
				require.NoError(t, s.Ping(ctx))
				return s
			}

			rw := open(false, 10*time.Second)
			_, err := rw.Exec(ctx, rawSQL{query: `DROP TABLE IF EXISTS extdb_it`})
			require.NoError(t, err)
			_, err = rw.Exec(ctx, rawSQL{query: `CREATE TABLE extdb_it (id INT PRIMARY KEY)`})
			require.NoError(t, err)
			t.Cleanup(func() {
				if _, err := rw.Exec(ctx, rawSQL{query: `DROP TABLE extdb_it`}); err != nil {
					t.Logf("drop extdb_it: %v", err)
				}
			})

			ro := open(true, 10*time.Second)
			_, err = ro.Exec(ctx, rw.Insert("extdb_it").Rows(map[string]any{"id": 1}))
			require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)

			if driver != DriverSQLServer {
				// 第二层：只读事务。SQL Server 没有只读事务，靠只读账号。
				err = ro.Query(ctx, rawSQL{query: `INSERT INTO extdb_it (id) VALUES (2)`}, func(*sql.Rows) error { return nil })
				require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)
			}
			var n int
			require.NoError(t, ro.Query(ctx, rw.From("extdb_it").Select(goqu.COUNT(goqu.Star())), func(r *sql.Rows) error { return r.Scan(&n) }))
			require.Equal(t, 0, n, "nothing was written through the read-only source")

			slow := open(true, 500*time.Millisecond)
			start := time.Now()
			err = slow.Query(ctx, rawSQL{query: sleepSQL[driver]}, func(*sql.Rows) error { return nil })
			require.True(t, externalV1.IsExternalSourceTimeout(err), "got %v", err)
			require.Less(t, time.Since(start), 2500*time.Millisecond)
		})
	}
}
