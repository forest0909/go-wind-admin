package service

import (
	"net/http/httptest"
	"testing"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
)

func headersOf[T any](cols []exportColumn[T]) []string {
	return colHeaders(cols)
}

// 审计日志导出列必须与列表页展示列一致（"导出的就是页面看到的"）。
// 策略评估日志的 logHash/signature 是完整性校验字段，页面不展示，不进导出。
func TestAuditExportColumnHeaders(t *testing.T) {
	cases := []struct {
		name string
		cols []string
		want []string
	}{
		{"policy_evaluation", headersOf(policyEvaluationExportColumns()), []string{
			"id", "createdAt", "tenantId", "userId", "membershipId", "permissionId",
			"policyId", "requestPath", "requestMethod", "result", "effectDetails",
			"scopeSql", "ipAddress", "traceId", "evaluationContext",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.cols) != len(c.want) {
				t.Fatalf("column count = %d, want %d", len(c.cols), len(c.want))
			}
			set := map[string]bool{}
			for _, h := range c.cols {
				set[h] = true
			}
			for _, w := range c.want {
				if !set[w] {
					t.Errorf("missing expected column header %q", w)
				}
			}
		})
	}
}

// parseExportRequest：format 白名单、maxRows 只能往下调、query 原样透传。
func TestParseExportRequest(t *testing.T) {
	t.Run("default format is xlsx and maxRows capped", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://x/export", nil)
		format, req, ok := parseExportRequest(w, r, 500000)
		if !ok || format != "xlsx" {
			t.Fatalf("format = %q ok = %v, want xlsx/true", format, ok)
		}
		if req.GetLimit() != 500000 {
			t.Fatalf("limit = %d, want 500000", req.GetLimit())
		}
		if !req.GetNoPaging() {
			t.Fatal("NoPaging must be true for export")
		}
	})
	t.Run("bad format rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://x/export?format=pdf", nil)
		_, _, ok := parseExportRequest(w, r, 500000)
		if ok {
			t.Fatal("format=pdf must be rejected")
		}
		if w.Result().StatusCode != 400 {
			t.Fatalf("status = %d, want 400", w.Result().StatusCode)
		}
	})
	t.Run("maxRows lowered but never raised", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://x/export?maxRows=100", nil)
		_, req, ok := parseExportRequest(w, r, 500000)
		if !ok {
			t.Fatal("valid maxRows must pass")
		}
		if req.GetLimit() != 100 {
			t.Fatalf("limit = %d, want 100", req.GetLimit())
		}

		w2 := httptest.NewRecorder()
		r2 := httptest.NewRequest("GET", "http://x/export?maxRows=999999999", nil)
		_, req2, ok2 := parseExportRequest(w2, r2, 500000)
		if !ok2 {
			t.Fatal("oversized maxRows must pass at cap")
		}
		if req2.GetLimit() != 500000 {
			t.Fatalf("limit = %d, want capped 500000", req2.GetLimit())
		}
	})
	t.Run("query passthrough", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://x/export?query=%7B%22tenantId%22%3A1%7D", nil)
		_, req, ok := parseExportRequest(w, r, 500000)
		if !ok {
			t.Fatal("query must pass through")
		}
		qf, okQ := req.GetFilteringType().(*paginationV1.PagingRequest_Query)
		if !okQ || qf.Query != `{"tenantId":1}` {
			t.Fatalf("query not passed through: %+v", req.GetFilteringType())
		}
	})
}
