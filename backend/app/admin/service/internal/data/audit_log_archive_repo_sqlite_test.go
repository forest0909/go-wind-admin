package data

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// newAuditLogArchiveRepoSqlite 用 enttest helper 构造一个可直接做归档的
// AuditLogArchiveRepo，逐字段复刻 NewAuditLogArchiveRepo（log 换 NopLogger，
// client 取测试 client 的底层 ent client；该 repo 无 init()）。
func newAuditLogArchiveRepoSqlite(t *testing.T) *AuditLogArchiveRepo {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	return &AuditLogArchiveRepo{
		log:    bLogger.NewHelper(bLogger.NopLogger()),
		client: entClient.Client(),
	}
}

// seedOneRowPerAuditTable 往登录日志和操作日志各直插一行。
func seedOneRowPerAuditTable(t *testing.T, client *ent.Client, ctx context.Context, suffix string) {
	t.Helper()
	require.NoError(t, client.LoginAuditLog.Create().
		SetCreatedAt(time.Now()).
		SetNillableUsername(trans.Ptr("archive_user_"+suffix)).
		Exec(ctx), "直插 sys_login_audit_logs 应成功")
	require.NoError(t, client.OperationAuditLog.Create().
		SetCreatedAt(time.Now()).
		SetNillableResourceType(trans.Ptr("archive_res_"+suffix)).
		Exec(ctx), "直插 sys_operation_audit_logs 应成功")
}

// auditTableCounts 直查登录日志和操作日志的当前行数。
func auditTableCounts(t *testing.T, client *ent.Client, ctx context.Context) map[string]int {
	t.Helper()
	counts := map[string]int{}
	var err error
	counts["sys_login_audit_logs"], err = client.LoginAuditLog.Query().Count(ctx)
	require.NoError(t, err)
	counts["sys_operation_audit_logs"], err = client.OperationAuditLog.Query().Count(ctx)
	require.NoError(t, err)
	return counts
}

// TestAuditLogArchiveRepoSqlite_ArchiveExpiredBeforeNow 验证：
// before 取未来时间时，登录日志和操作日志（created_at 为当前时间，均早于 before）
// 全部导出为 JSONL 后从库内删除，返回各表归档行数；落盘文件逐表一份、
// 行数与导出一致；库内两表计数归零。
func TestAuditLogArchiveRepoSqlite_ArchiveExpiredBeforeNow(t *testing.T) {
	repo := newAuditLogArchiveRepoSqlite(t)
	client := repo.client
	ctx := enttest.NewSystemContext(context.Background())

	seedOneRowPerAuditTable(t, client, ctx, "future")
	kept := map[string]int{
		"sys_login_audit_logs":     1,
		"sys_operation_audit_logs": 1,
	}
	require.Equal(t, kept, auditTableCounts(t, client, ctx), "两张审计表应各含 1 行")

	outDir := filepath.Join(t.TempDir(), "archive")
	results, err := repo.ArchiveExpired(ctx, time.Now().Add(1*time.Hour), outDir)
	require.NoError(t, err, "归档应成功")
	require.NoError(t, err, "归档应成功")
	require.Equal(t, kept, results, "两张表各归档 1 行")

	require.Equal(t, map[string]int{
		"sys_login_audit_logs":     0,
		"sys_operation_audit_logs": 0,
	}, auditTableCounts(t, client, ctx), "归档后两张表计数应归零")

	// 落盘校验：每张表一份 JSONL，各含 1 行合法 JSON。
	// 文件名为 <表名>-<stamp>.jsonl（stamp 形如 20060102-150405，自带连字符）。
	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		files[e.Name()] = filepath.Join(outDir, e.Name())
	}
	require.Len(t, files, 2, "应落盘 2 份 JSONL 文件")
	tables := map[string]bool{}
	for name, path := range files {
		table := ""
		for _, tbl := range []string{
			"sys_login_audit_logs", "sys_operation_audit_logs",
		} {
			if strings.HasPrefix(name, tbl+"-") {
				table = tbl
				break
			}
		}
		require.NotEmpty(t, table, "文件 %s 应以登录或操作日志表名为前缀", name)
		tables[table] = true
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		require.Len(t, lines, 1, "文件 %s 应恰含 1 行（导出行数与删除行数一致）", name)
		require.True(t, json.Valid([]byte(lines[0])), "归档行应为合法 JSON")
	}
	require.Len(t, tables, 2, "两张审计表应各落盘一份")
}

// TestAuditLogArchiveRepoSqlite_ArchiveExpiredAfterNow 验证：
// before 取过去时间时，无行满足 created_at < before，
// 不归档、不落盘、库内行保留，返回空 map。
func TestAuditLogArchiveRepoSqlite_ArchiveExpiredAfterNow(t *testing.T) {
	repo := newAuditLogArchiveRepoSqlite(t)
	client := repo.client
	ctx := enttest.NewSystemContext(context.Background())

	seedOneRowPerAuditTable(t, client, ctx, "past")

	outDir := filepath.Join(t.TempDir(), "archive")
	results, err := repo.ArchiveExpired(ctx, time.Now().Add(-24*time.Hour), outDir)
	require.NoError(t, err, "空跑归档应成功")
	require.Empty(t, results, "过去时间阈值应无归档行")

	require.Equal(t, map[string]int{
		"sys_login_audit_logs":     1,
		"sys_operation_audit_logs": 1,
	}, auditTableCounts(t, client, ctx), "空跑后两张表行数应保留")

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	jsonlCount := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			jsonlCount++
		}
	}
	require.Zero(t, jsonlCount, "空跑不应落盘任何 JSONL 文件")
}
