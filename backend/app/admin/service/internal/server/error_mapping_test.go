package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/dicttype"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

func runErrorMapping(t *testing.T, err error) *kratosErrors.Error {
	t.Helper()
	h := ErrorMapping(bLogger.NewHelper(bLogger.NopLogger()))(func(ctx context.Context, req any) (any, error) {
		return nil, err
	})
	_, got := h(context.Background(), nil)
	return kratosErrors.FromError(got)
}

// 真实 ent 错误经过中间件后的状态码与 reason。修复前没有这层映射，
// kratos 默认编码把它们全部变成 500、reason 为空、message 为 ent 原文。
func TestErrorMapping_EntErrors(t *testing.T) {
	client := enttest.NewEntClientForTest(t).Client()
	ctx := enttest.NewSystemContext(context.Background())

	_, notFound := client.DictType.Query().Where(dicttype.IDEQ(999999)).Only(ctx)
	if notFound == nil {
		t.Fatal("expected not found error")
	}
	e := runErrorMapping(t, fmt.Errorf("wrapped: %w", notFound))
	if e.Code != 404 || e.Reason != "NOT_FOUND" || strings.Contains(e.Message, "ent:") {
		t.Fatalf("not found mapped to %d %q %q", e.Code, e.Reason, e.Message)
	}

	if _, err := client.DictType.Create().SetTypeCode("dup").SetTypeName("a").Save(ctx); err != nil {
		t.Fatal(err)
	}
	_, conflict := client.DictType.Create().SetTypeCode("dup").SetTypeName("b").Save(ctx)
	if conflict == nil {
		t.Fatal("expected unique constraint error")
	}
	e = runErrorMapping(t, conflict)
	if e.Code != 409 || e.Reason != "CONFLICT" || strings.Contains(e.Message, "UNIQUE") {
		t.Fatalf("constraint mapped to %d %q %q", e.Code, e.Reason, e.Message)
	}
}

func TestErrorMapping_PlainErrorHidesText(t *testing.T) {
	e := runErrorMapping(t, errors.New("pq: secret column detail"))
	if e.Code != 500 || e.Reason == "" || strings.Contains(e.Message, "secret") {
		t.Fatalf("plain error mapped to %d %q %q", e.Code, e.Reason, e.Message)
	}
}

func TestErrorMapping_KratosErrorPassThrough(t *testing.T) {
	e := runErrorMapping(t, fmt.Errorf("ctx: %w", adminV1.ErrorBadRequest("bad thing")))
	if e.Code != 400 || e.Reason != "BAD_REQUEST" || e.Message != "bad thing" {
		t.Fatalf("kratos error changed to %d %q %q", e.Code, e.Reason, e.Message)
	}
}
