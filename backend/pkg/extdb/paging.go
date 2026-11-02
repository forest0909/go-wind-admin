package extdb

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-crud/pagination/filter"
	"go-wind-admin/third_party/go-crud/pagination/sorting"
	"go-wind-admin/third_party/go-utils/stringcase"
)

// 分页上限 / Paging limits.
const (
	DefaultPageSize = 10
	MaxPageSize     = 100  // page_size 超过即截断
	MaxNoPagingRows = 1000 // no_paging=true 时最多返回的行数
)

// ColumnType 列的值类型，决定过滤值怎么转换、哪些操作符可用。
// ColumnType decides how filter values are converted and which operators apply.
type ColumnType int

const (
	TypeString  ColumnType = iota // 字符串：支持 contains 等模糊匹配
	TypeBool                      // 布尔（SQL Server bit、MySQL tinyint(1)）
	TypeInt                       // 整数
	TypeDecimal                   // 定点数：值保持十进制字符串，不转 float
	TypeTime                      // 无时区时间：按数据源时区解释
)

// Column 白名单里的一列 / One whitelisted column.
type Column struct {
	Name string // 外部库里的列名
	Type ColumnType
}

// Table 一张外部表的白名单：只有 Columns 里登记的 API 字段能过滤、排序。
// Table is the whitelist for one external table: only fields in Columns may be filtered or sorted.
type Table struct {
	Name    string            // 外部表名
	PK      string            // 主键对应的 API 字段名；无排序时按它排，有排序时追加它保证分页稳定
	Columns map[string]Column // API 字段名（snake_case）→ 外部列
}

// Page 把 PagingRequest 翻译成列表查询和计数查询。
// ds 是 s.From(...) 起头、已经 Select 好列的数据集；过滤和排序字段必须在 t 的白名单里，否则 400。
// Page translates a PagingRequest into a list query and a count query.
func (s *Source) Page(ds *goqu.SelectDataset, t *Table, req *paginationV1.PagingRequest) (list, count *goqu.SelectDataset, err error) {
	if _, ok := t.Columns[t.PK]; !ok {
		return nil, nil, s.buildError(context.Background(), errPKNotWhitelisted(t))
	}
	fe, err := filter.ConvertFilterByPagingRequest(req)
	if err != nil {
		return nil, nil, ErrBadRequest("invalid filter: %v", err)
	}
	if fe != nil {
		where, err := s.buildFilterExpr(t, fe)
		if err != nil {
			return nil, nil, err
		}
		if where != nil {
			ds = ds.Where(where)
		}
	}
	count = ds.Select(goqu.COUNT(goqu.Star()))

	order, err := s.buildOrder(t, req)
	if err != nil {
		return nil, nil, err
	}
	list = ds.Order(order...)

	if req.GetNoPaging() {
		return list.Limit(MaxNoPagingRows), count, nil
	}
	page := uint(req.GetPage())
	if page < 1 {
		page = 1
	}
	size := uint(req.GetPageSize())
	if size < 1 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	return list.Offset((page - 1) * size).Limit(size), count, nil
}

func (s *Source) buildOrder(t *Table, req *paginationV1.PagingRequest) ([]exp.OrderedExpression, error) {
	sorts := req.GetSorting()
	if req.GetOrderBy() != "" {
		parsed, err := sorting.NewOrderByStringConverter().Convert(req.GetOrderBy())
		if err != nil {
			return nil, ErrBadRequest("invalid orderBy: %v", err)
		}
		sorts = append(sorts, parsed...)
	}
	var out []exp.OrderedExpression
	hasPK := false
	for _, so := range sorts {
		field := stringcase.ToSnakeCase(so.GetField())
		col, ok := t.Columns[field]
		if !ok {
			return nil, ErrBadRequest("field %q cannot be sorted", so.GetField())
		}
		if field == t.PK {
			hasPK = true
		}
		if so.GetDirection() == paginationV1.Sorting_DESC {
			out = append(out, goqu.C(col.Name).Desc())
		} else {
			out = append(out, goqu.C(col.Name).Asc())
		}
	}
	if !hasPK {
		out = append(out, goqu.C(t.Columns[t.PK].Name).Asc())
	}
	return out, nil
}

func (s *Source) buildFilterExpr(t *Table, fe *paginationV1.FilterExpr) (exp.Expression, error) {
	var parts []exp.Expression
	for _, c := range fe.GetConditions() {
		e, err := s.buildCondition(t, c)
		if err != nil {
			return nil, err
		}
		if e != nil {
			parts = append(parts, e)
		}
	}
	for _, g := range fe.GetGroups() {
		e, err := s.buildFilterExpr(t, g)
		if err != nil {
			return nil, err
		}
		if e != nil {
			parts = append(parts, e)
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	if fe.GetType() == paginationV1.ExprType_OR {
		return goqu.Or(parts...), nil
	}
	return goqu.And(parts...), nil
}

func (s *Source) buildCondition(t *Table, c *paginationV1.FilterCondition) (exp.Expression, error) {
	field := stringcase.ToSnakeCase(c.GetField())
	col, ok := t.Columns[field]
	if !ok {
		return nil, ErrBadRequest("field %q cannot be filtered", c.GetField())
	}
	if c.JsonPath != nil || c.DatePart != nil {
		return nil, ErrBadRequest("field %q: json path and date part filters are not supported", c.GetField())
	}
	ident := goqu.C(col.Name)
	raw := conditionValue(c)

	switch c.GetOp() {
	case paginationV1.Operator_EQ, paginationV1.Operator_EXACT,
		paginationV1.Operator_NEQ,
		paginationV1.Operator_GT, paginationV1.Operator_GTE,
		paginationV1.Operator_LT, paginationV1.Operator_LTE:
		v, err := s.convertValue(field, col, raw)
		if err != nil {
			return nil, err
		}
		return compare(ident, c.GetOp(), v, col)

	case paginationV1.Operator_IN, paginationV1.Operator_NIN:
		raws, err := conditionValues(c)
		if err != nil {
			return nil, err
		}
		if len(raws) == 0 {
			return nil, ErrBadRequest("field %q: empty value list", c.GetField())
		}
		vals := make([]any, 0, len(raws))
		for _, r := range raws {
			v, err := s.convertValue(field, col, r)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		if c.GetOp() == paginationV1.Operator_IN {
			return ident.In(vals...), nil
		}
		return ident.NotIn(vals...), nil

	case paginationV1.Operator_BETWEEN:
		raws, err := conditionValues(c)
		if err != nil {
			return nil, err
		}
		if len(raws) != 2 {
			return nil, ErrBadRequest("field %q: between needs two values", c.GetField())
		}
		lo, err := s.convertValue(field, col, raws[0])
		if err != nil {
			return nil, err
		}
		hi, err := s.convertValue(field, col, raws[1])
		if err != nil {
			return nil, err
		}
		return goqu.And(ident.Gte(lo), ident.Lte(hi)), nil

	case paginationV1.Operator_IS_NULL:
		return ident.IsNull(), nil
	case paginationV1.Operator_IS_NOT_NULL:
		return ident.IsNotNull(), nil

	case paginationV1.Operator_CONTAINS, paginationV1.Operator_ICONTAINS,
		paginationV1.Operator_STARTS_WITH, paginationV1.Operator_ISTARTS_WITH,
		paginationV1.Operator_ENDS_WITH, paginationV1.Operator_IENDS_WITH,
		paginationV1.Operator_IEXACT:
		if col.Type != TypeString {
			return nil, ErrBadRequest("field %q: fuzzy match only applies to text fields", c.GetField())
		}
		if raw == "" {
			return nil, nil // 空搜索词不加条件
		}
		return s.likeExpr(col.Name, likePattern(s.escapeLike(raw), c.GetOp())), nil
	}
	return nil, ErrBadRequest("field %q: operator %s is not supported", c.GetField(), c.GetOp())
}

// likeExpr 不区分大小写的 LIKE：PG 用 ILIKE；MySQL、SQL Server 用 LIKE，大小写按列的排序规则
// （常见的 _ci 排序规则不区分大小写）。
func (s *Source) likeExpr(col string, pattern string) exp.Expression {
	if s.driver == driverSQLite {
		// SQLite 的 LIKE 没有默认转义字符，要显式写 ESCAPE。
		return goqu.L(`? LIKE ? ESCAPE '\'`, goqu.C(col), pattern)
	}
	return goqu.C(col).ILike(pattern)
}

// escapeLike 转义 LIKE 通配符，让用户输入的 % 和 _ 按字面匹配。
// PG、MySQL 的 LIKE 默认转义字符是反斜杠；SQL Server 没有默认转义字符，用方括号。
func (s *Source) escapeLike(v string) string {
	if s.driver == DriverSQLServer {
		return strings.NewReplacer(`[`, `[[]`, `%`, `[%]`, `_`, `[_]`).Replace(v)
	}
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(v)
}

func likePattern(escaped string, op paginationV1.Operator) string {
	switch op {
	case paginationV1.Operator_STARTS_WITH, paginationV1.Operator_ISTARTS_WITH:
		return escaped + "%"
	case paginationV1.Operator_ENDS_WITH, paginationV1.Operator_IENDS_WITH:
		return "%" + escaped
	case paginationV1.Operator_IEXACT:
		return escaped
	default:
		return "%" + escaped + "%"
	}
}

func compare(ident exp.IdentifierExpression, op paginationV1.Operator, v any, col Column) (exp.Expression, error) {
	if col.Type == TypeBool {
		// goqu 把布尔比较渲染成 IS TRUE，SQL Server 不支持；包成参数走普通的 = / !=。
		v = goqu.L("?", v)
		switch op {
		case paginationV1.Operator_EQ, paginationV1.Operator_EXACT:
			return ident.Eq(v), nil
		case paginationV1.Operator_NEQ:
			return ident.Neq(v), nil
		default:
			return nil, ErrBadRequest("operator %s does not apply to boolean fields", op)
		}
	}
	switch op {
	case paginationV1.Operator_EQ, paginationV1.Operator_EXACT:
		return ident.Eq(v), nil
	case paginationV1.Operator_NEQ:
		return ident.Neq(v), nil
	case paginationV1.Operator_GT:
		return ident.Gt(v), nil
	case paginationV1.Operator_GTE:
		return ident.Gte(v), nil
	case paginationV1.Operator_LT:
		return ident.Lt(v), nil
	default:
		return ident.Lte(v), nil
	}
}

var decimalRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// timeLayouts 过滤值接受的时间格式；不带时区的按数据源时区解释。
var timeLayouts = []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"}

// convertValue 把过滤值按列类型转换成驱动参数，格式不对返回 400。
func (s *Source) convertValue(field string, col Column, raw string) (any, error) {
	switch col.Type {
	case TypeBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, ErrBadRequest("field %q: %q is not a boolean", field, raw)
		}
		return b, nil
	case TypeInt:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, ErrBadRequest("field %q: %q is not an integer", field, raw)
		}
		return n, nil
	case TypeDecimal:
		if !decimalRe.MatchString(raw) {
			return nil, ErrBadRequest("field %q: %q is not a decimal", field, raw)
		}
		return raw, nil
	case TypeTime:
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return s.TimeToDB(t), nil
		}
		for _, layout := range timeLayouts {
			if t, err := time.ParseInLocation(layout, raw, s.loc); err == nil {
				return s.TimeToDB(t), nil
			}
		}
		return nil, ErrBadRequest("field %q: %q is not a time", field, raw)
	default:
		return raw, nil
	}
}

func conditionValue(c *paginationV1.FilterCondition) string {
	if jv := c.GetJsonValue(); jv != nil {
		switch v := jv.AsInterface().(type) {
		case string:
			return v
		case bool:
			return strconv.FormatBool(v)
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return c.GetValue()
}

// conditionValues IN / BETWEEN 的值：优先 values；否则把 value 当 JSON 数组解析（query 串的写法）。
func conditionValues(c *paginationV1.FilterCondition) ([]string, error) {
	if len(c.GetValues()) > 0 {
		return c.GetValues(), nil
	}
	raw := conditionValue(c)
	if raw == "" {
		return nil, nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, ErrBadRequest("field %q: value must be a JSON array", c.GetField())
	}
	out := make([]string, 0, len(arr))
	for _, a := range arr {
		switch v := a.(type) {
		case string:
			out = append(out, v)
		case bool:
			out = append(out, strconv.FormatBool(v))
		case float64:
			out = append(out, strconv.FormatFloat(v, 'f', -1, 64))
		default:
			return nil, ErrBadRequest("field %q: unsupported value in array", c.GetField())
		}
	}
	return out, nil
}

type pkError struct{ table, pk string }

func (e pkError) Error() string {
	return "table " + e.table + ": primary key field " + e.pk + " is not in the column whitelist"
}

func errPKNotWhitelisted(t *Table) error { return pkError{table: t.Name, pk: t.PK} }
