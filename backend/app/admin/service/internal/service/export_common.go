package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"go-wind-admin/third_party/go-utils/timeutil"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/timestamppb"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	paginationFilter "go-wind-admin/third_party/go-crud/pagination/filter"
	"go-wind-admin/third_party/go-crud/pagination/paginator"

	"go-wind-admin/third_party/go-crud/viewer"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/pkg/middleware/auth"
)

// exportColumn 一列的定义：表头 + 从行 DTO 取值的取值器（返回已格式化的字符串）。
// 各导出资源（登录日志 / 操作日志）共用此形态。
type exportColumn[T any] struct {
	header string
	value  func(row T) string
}

func colHeaders[T any](cols []exportColumn[T]) []string {
	headers := make([]string, 0, len(cols))
	for _, c := range cols {
		headers = append(headers, c.header)
	}
	return headers
}

func colValues[T any](cols []exportColumn[*T], row *T) []string {
	values := make([]string, 0, len(cols))
	for _, c := range cols {
		values = append(values, c.value(row))
	}
	return values
}

func writeCSV(w http.ResponseWriter, headers []string, rows [][]string) error {
	// 手写 CSV（RFC 4180）：含引号/逗号/换行的字段加引号转义；UTF-8 BOM 让 Excel 正确识别中文
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	escape := func(v string) string {
		if strings.ContainsAny(v, ",\"\n\r") {
			return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
		}
		return v
	}
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, joinCSV(headers, escape))
	for _, row := range rows {
		lines = append(lines, joinCSV(row, escape))
	}
	_, err := w.Write([]byte(strings.Join(lines, "\r\n")))
	return err
}

func joinCSV(fields []string, escape func(string) string) string {
	out := make([]byte, 0, 32*len(fields))
	for i, f := range fields {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, escape(f)...)
	}
	return string(out)
}

func buildXLSX(sheet string, headers []string, rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if _, err := f.NewSheet(sheet); err != nil {
		return nil, err
	}
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	cell, _ := excelize.CoordinatesToCellName(1, 1)
	if err := f.SetSheetRow(sheet, cell, &headers); err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		cell, _ = excelize.CoordinatesToCellName(1, 2)
		if err := f.SetSheetRow(sheet, cell, &rows); err != nil {
			return nil, err
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fmtTime(ts *timestamppb.Timestamp) string {
	t := timeutil.TimestamppbToTime(ts)
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func boolStr(v *bool) string {
	if v == nil {
		return ""
	}
	return strconv.FormatBool(*v)
}

// ==== 手动导出路由的公共样板 ====
//
// :export 手动路由不穿 kratos middleware（proto 生成 handler 才走中间件链），
// 因此 auth 中间件的一套（token 校验、操作人注入、ent viewer 注入）对它全部缺席。
// 各导出 handler 在此补齐：token 自校验 + 操作人注入 + viewer 重建。

// authorizeExportRequest 校验 Bearer token 并重建 auth/ent 两层上下文。
// 返回 nil 表示已写好 401 响应，调用方直接 return。
//
// viewer 重建：单租户下一律 SystemContext。
// 是否放行租户用户由各 handler 在拿到 operator 后自行定夺（审计/台账=平台闸）。
func authorizeExportRequest(
	w http.ResponseWriter,
	r *http.Request,
	tokenChecker auth.AccessTokenChecker,
) (context.Context, *authenticationV1.UserTokenPayload, bool) {
	authzHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authzHeader, "Bearer ")
	if token == "" || token == authzHeader {
		http.Error(w, "unauthorized: bearer token required", http.StatusUnauthorized)
		return nil, nil, false
	}
	valid, operator := tokenChecker.IsValidAccessToken(r.Context(), token, false)
	if !valid || operator == nil {
		http.Error(w, "unauthorized: invalid token", http.StatusUnauthorized)
		return nil, nil, false
	}
	ctx := auth.NewContext(r.Context(), operator)

	ctx = viewer.WithSystemContext(ctx)
	return ctx, operator, true
}

// requireExportPlatformAdmin 平台管理员闸（
// 手动路由拿不到 service 层 ctx，须在 operator 已解析处就地判定）。
func requireExportPlatformAdmin(w http.ResponseWriter, operator *authenticationV1.UserTokenPayload) bool {
	if !operator.GetIsPlatformAdmin() {
		http.Error(w, "forbidden: platform admin only", http.StatusForbidden)
		return false
	}
	return true
}

// parseExportRequest 解析导出参数：format（xlsx/csv，缺省 xlsx）与行数上限
// （cap 为该资源的硬顶，maxRows 只能往下调，放在 req.Limit 里）。
// query JSON（列表页同源条件）原样带上，由 collectExportRows 和 id 游标组合后分批查询，
// "导出的就是当前搜索看到的"。
func parseExportRequest(w http.ResponseWriter, r *http.Request, cap int) (string, *paginationV1.PagingRequest, bool) {
	q := r.URL.Query()
	format := strings.ToLower(q.Get("format"))
	if format == "" {
		format = "xlsx"
	}
	if format != "xlsx" && format != "csv" {
		http.Error(w, "format must be xlsx or csv", http.StatusBadRequest)
		return "", nil, false
	}
	maxRows := cap
	if v := q.Get("maxRows"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed < cap {
			maxRows = parsed
		}
	}

	req := &paginationV1.PagingRequest{
		Limit: trans.Ptr(uint32(maxRows)),
	}
	if query := strings.TrimSpace(q.Get("query")); query != "" {
		req.FilteringType = &paginationV1.PagingRequest_Query{Query: query}
	}
	return format, req, true
}

// errExportBadQuery 导出的 query 解析失败。调用方回 400：
// 不能像列表接口那样丢掉条件继续查，否则一个写坏的后缀就导出全表。
var errExportBadQuery = errors.New("invalid export query")

// collectExportRows 按 id 倒序（最新在前）、用 id 游标分批拉取导出数据，最多 req.Limit 行。
// truncated 为 true 表示符合条件的数据超过上限、没有全部导出。
//
// 以前一次 NoPaging 查询了事：go-crud 的 NoPaging 兜底上限是 1 万行、而且忽略 Limit，
// 又没有 ORDER BY，导出的是最旧的 1 万行，maxRows 和 50 万上限都不生效。
// 分批时：剩余量不超过 paginator.MaxLimit 就按剩余量取一页，否则走 NoPaging（每批至多
// NoPagingMaxLimit 行），取回 0 行才结束。多取一行用来判断是否截断。
func collectExportRows[T any](
	ctx context.Context,
	req *paginationV1.PagingRequest,
	fetch func(ctx context.Context, req *paginationV1.PagingRequest) ([]T, error),
	idOf func(T) uint32,
) (rows []T, truncated bool, err error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		return nil, false, fmt.Errorf("export limit must be positive, got %d", limit)
	}

	userFilter, err := paginationFilter.ConvertFilterByPagingRequest(req)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", errExportBadQuery, err)
	}

	var cursor uint32
	for len(rows) <= limit {
		expr := &paginationV1.FilterExpr{Type: paginationV1.ExprType_AND}
		if userFilter != nil {
			expr.Groups = append(expr.Groups, userFilter)
		}
		if cursor > 0 {
			expr.Conditions = append(expr.Conditions, &paginationV1.FilterCondition{
				Field:      "id",
				Op:         paginationV1.Operator_LT,
				ValueOneof: &paginationV1.FilterCondition_Value{Value: strconv.FormatUint(uint64(cursor), 10)},
			})
		}
		batchReq := &paginationV1.PagingRequest{
			Sorting:       []*paginationV1.Sorting{{Field: "id", Direction: paginationV1.Sorting_DESC}},
			FilteringType: &paginationV1.PagingRequest_FilterExpr{FilterExpr: expr},
		}
		if need := limit + 1 - len(rows); need <= paginator.MaxLimit {
			batchReq.Page = trans.Ptr(uint32(1))
			batchReq.PageSize = trans.Ptr(uint32(need))
		} else {
			batchReq.NoPaging = trans.Ptr(true)
		}

		batch, ferr := fetch(ctx, batchReq)
		if ferr != nil {
			return nil, false, ferr
		}
		if len(batch) == 0 {
			break
		}
		next := idOf(batch[len(batch)-1])
		if cursor > 0 && next >= cursor {
			return nil, false, fmt.Errorf("export cursor did not advance (cursor %d, got %d)", cursor, next)
		}
		cursor = next
		rows = append(rows, batch...)
	}

	if len(rows) > limit {
		return rows[:limit], true, nil
	}
	return rows, false, nil
}

// setExportTruncationHeaders 截断时在响应头里写明，前端据此提示"只导出了前 N 行"。
// 跨域部署（独立 API 域）时这些头必须 expose 才读得到，Content-Disposition 同理。
func setExportTruncationHeaders(w http.ResponseWriter, truncated bool, limit int) {
	w.Header().Add("Access-Control-Expose-Headers", "Content-Disposition, X-Export-Truncated, X-Export-Row-Limit")
	if truncated {
		w.Header().Set("X-Export-Truncated", "true")
		w.Header().Set("X-Export-Row-Limit", strconv.Itoa(limit))
	}
}

// emitExportFile 把列化的数据按 format 写成下载响应。
// 查询/构建的原始错误由调用方留日志（仓铁律：不吞错），此处只回笼统文案。
func emitExportFile(
	w http.ResponseWriter,
	log *logger.Helper,
	ctx context.Context,
	tag string,
	fileName string,
	sheet string,
	headers []string,
	rows [][]string,
	format string,
) {
	stamp := time.Now().Format("20060102_150405")

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.csv"`, fileName, stamp))
		if err := writeCSV(w, headers, rows); err != nil {
			log.Errorf(ctx, "export [%s] csv write failed: %s", tag, err.Error())
			return
		}
	default:
		data, err := buildXLSX(sheet, headers, rows)
		if err != nil {
			log.Errorf(ctx, "export [%s] xlsx build failed: %s", tag, err.Error())
			http.Error(w, "build xlsx failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.xlsx"`, fileName, stamp))
		_, _ = w.Write(data)
	}
}
