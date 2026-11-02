package service

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-crud/pagination/paginator"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

func seedLoginAuditLogs(t *testing.T, n int) (*data.LoginAuditLogRepo, context.Context) {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	repo := data.NewLoginAuditLogRepo(boot, entClient)
	for i := 1; i <= n; i++ {
		kind := "odd"
		if i%2 == 0 {
			kind = "even"
		}
		require.NoError(t, repo.Create(ctx, &auditV1.CreateLoginAuditLogRequest{Data: &auditV1.LoginAuditLog{
			Username:  trans.Ptr(fmt.Sprintf("%s-%03d", kind, i)),
			RequestId: trans.Ptr(fmt.Sprintf("req-%d", i)),
		}}))
	}
	return repo, ctx
}

func collectLogins(ctx context.Context, repo *data.LoginAuditLogRepo, req *paginationV1.PagingRequest) ([]*auditV1.LoginAuditLog, bool, error) {
	return collectExportRows(ctx, req,
		func(ctx context.Context, r *paginationV1.PagingRequest) ([]*auditV1.LoginAuditLog, error) {
			resp, err := repo.List(ctx, r)
			return resp.GetItems(), err
		},
		func(row *auditV1.LoginAuditLog) uint32 { return row.GetId() },
	)
}

func requireIDsDesc(t *testing.T, rows []*auditV1.LoginAuditLog, firstID uint32) {
	t.Helper()
	for i, r := range rows {
		require.Equal(t, firstID-uint32(i), r.GetId(), "rows must be newest first without gaps (index %d)", i)
	}
}

// 修复前：NoPaging 一次查询，忽略 Limit（maxRows=5 照样导出全部）、无 ORDER BY（最旧在前）。
func TestCollectExportRows_LimitHonoredNewestFirst(t *testing.T) {
	repo, ctx := seedLoginAuditLogs(t, 250)

	rows, truncated, err := collectLogins(ctx, repo, &paginationV1.PagingRequest{Limit: trans.Ptr(uint32(5))})
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, rows, 5)
	requireIDsDesc(t, rows, 250)

	rows, truncated, err = collectLogins(ctx, repo, &paginationV1.PagingRequest{Limit: trans.Ptr(uint32(1000))})
	require.NoError(t, err)
	require.False(t, truncated, "everything fits under the limit")
	require.Len(t, rows, 250)
	requireIDsDesc(t, rows, 250)

	rows, truncated, err = collectLogins(ctx, repo, &paginationV1.PagingRequest{Limit: trans.Ptr(uint32(250))})
	require.NoError(t, err)
	require.False(t, truncated, "exactly at the limit is not truncation")
	require.Len(t, rows, 250)
}

// 数据量超过 NoPaging 兜底上限（生产 1 万，这里缩到 30）时，游标跨批继续拉，上限按 Limit 算。
// 修复前这里只能拿到最旧的 30 行。
func TestCollectExportRows_CrossesNoPagingCap(t *testing.T) {
	repo, ctx := seedLoginAuditLogs(t, 250)
	old := paginator.NoPagingMaxLimit
	paginator.NoPagingMaxLimit = 30
	t.Cleanup(func() { paginator.NoPagingMaxLimit = old })

	rows, truncated, err := collectLogins(ctx, repo, &paginationV1.PagingRequest{Limit: trans.Ptr(uint32(200))})
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, rows, 200)
	requireIDsDesc(t, rows, 250)
}

func TestCollectExportRows_UserQueryKeptAndBadQueryRejected(t *testing.T) {
	repo, ctx := seedLoginAuditLogs(t, 40)

	rows, truncated, err := collectLogins(ctx, repo, &paginationV1.PagingRequest{
		Limit:         trans.Ptr(uint32(1000)),
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"username__contains":"even"}`},
	})
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, rows, 20)
	for _, r := range rows {
		require.Contains(t, r.GetUsername(), "even")
	}

	// 写坏的后缀：列表接口会丢条件返回全量，导出必须拒绝，否则导出全表。
	_, _, err = collectLogins(ctx, repo, &paginationV1.PagingRequest{
		Limit:         trans.Ptr(uint32(1000)),
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"username__badop":"even"}`},
	})
	require.True(t, errors.Is(err, errExportBadQuery), "got %v", err)
}

func TestSetExportTruncationHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setExportTruncationHeaders(w, true, 500)
	require.Equal(t, "true", w.Header().Get("X-Export-Truncated"))
	require.Equal(t, "500", w.Header().Get("X-Export-Row-Limit"))

	w = httptest.NewRecorder()
	setExportTruncationHeaders(w, false, 500)
	require.Empty(t, w.Header().Get("X-Export-Truncated"))
}
