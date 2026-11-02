package extdb

import (
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
)

var testTable = &Table{
	Name: "ext_demo_contacts",
	PK:   "id",
	Columns: map[string]Column{
		"id":         {Name: "id", Type: TypeString},
		"name":       {Name: "contact_name", Type: TypeString},
		"is_active":  {Name: "is_active", Type: TypeBool},
		"amount":     {Name: "amount", Type: TypeDecimal},
		"created_at": {Name: "created_at", Type: TypeTime},
	},
}

func dialectSource(t *testing.T, driver string) *Source {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	s, err := NewSource("demo", driver, nil, Options{Location: loc})
	require.NoError(t, err)
	return s
}

func renderPage(t *testing.T, s *Source, req *paginationV1.PagingRequest) (string, []any, string, []any) {
	t.Helper()
	list, count, err := s.Page(s.From(testTable.Name).Select("id", "contact_name"), testTable, req)
	require.NoError(t, err)
	lq, la, err := list.ToSQL()
	require.NoError(t, err)
	cq, ca, err := count.ToSQL()
	require.NoError(t, err)
	return lq, la, cq, ca
}

func ptr[T any](v T) *T { return &v }

func TestPage_ContainsPerDialect(t *testing.T) {
	req := &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"name__contains":"50%_off[x]\\"}`},
		OrderBy:       ptr(`["-name"]`),
		Page:          ptr(uint32(3)),
		PageSize:      ptr(uint32(20)),
	}
	cases := map[string]struct {
		list    string
		pattern string
		count   string
	}{
		DriverPostgres: {
			list:    `SELECT "id", "contact_name" FROM "ext_demo_contacts" WHERE ("contact_name" ILIKE $1) ORDER BY "contact_name" DESC, "id" ASC LIMIT $2 OFFSET $3`,
			pattern: `%50\%\_off[x]\\%`,
			count:   `SELECT COUNT(*) FROM "ext_demo_contacts" WHERE ("contact_name" ILIKE $1)`,
		},
		DriverMySQL: {
			list:    "SELECT `id`, `contact_name` FROM `ext_demo_contacts` WHERE (`contact_name` LIKE ?) ORDER BY `contact_name` DESC, `id` ASC LIMIT ? OFFSET ?",
			pattern: `%50\%\_off[x]\\%`,
			count:   "SELECT COUNT(*) FROM `ext_demo_contacts` WHERE (`contact_name` LIKE ?)",
		},
		DriverSQLServer: {
			list:    `SELECT "id", "contact_name" FROM "ext_demo_contacts" WHERE ("contact_name" LIKE @p1) ORDER BY "contact_name" DESC, "id" ASC OFFSET @p2 ROWS FETCH FIRST @p3 ROWS ONLY`,
			pattern: `%50[%][_]off[[]x]\%`,
			count:   `SELECT COUNT(*) FROM "ext_demo_contacts" WHERE ("contact_name" LIKE @p1)`,
		},
	}
	for driver, want := range cases {
		t.Run(driver, func(t *testing.T) {
			lq, la, cq, ca := renderPage(t, dialectSource(t, driver), req)
			require.Equal(t, want.list, lq)
			require.Equal(t, want.count, cq)
			require.Equal(t, want.pattern, la[0])
			require.Equal(t, []any{want.pattern}, ca)
			if driver == DriverSQLServer {
				require.Equal(t, []any{want.pattern, int64(40), int64(20)}, la)
			} else {
				require.Equal(t, []any{want.pattern, int64(20), int64(40)}, la)
			}
		})
	}
}

func TestPage_DefaultOrderAndPageSizeCap(t *testing.T) {
	s := dialectSource(t, DriverPostgres)
	lq, la, _, _ := renderPage(t, s, &paginationV1.PagingRequest{PageSize: ptr(uint32(100000))})
	require.Equal(t, `SELECT "id", "contact_name" FROM "ext_demo_contacts" ORDER BY "id" ASC LIMIT $1`, lq)
	require.Equal(t, []any{int64(MaxPageSize)}, la)

	// 排序里已有主键就不再追加
	lq, _, _, _ = renderPage(t, s, &paginationV1.PagingRequest{OrderBy: ptr(`["-id"]`)})
	require.Equal(t, `SELECT "id", "contact_name" FROM "ext_demo_contacts" ORDER BY "id" DESC LIMIT $1`, lq)

	lq, la, _, _ = renderPage(t, s, &paginationV1.PagingRequest{NoPaging: ptr(true)})
	require.Equal(t, `SELECT "id", "contact_name" FROM "ext_demo_contacts" ORDER BY "id" ASC LIMIT $1`, lq)
	require.Equal(t, []any{int64(MaxNoPagingRows)}, la)
}

func TestPage_TypedValues(t *testing.T) {
	s := dialectSource(t, DriverSQLServer)
	req := &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_Query{
			Query: `{"is_active":"true","amount__gte":"10.50","createdAt__gte":"2026-01-02 08:00:00"}`,
		},
	}
	lq, la, _, _ := renderPage(t, s, req)
	require.Contains(t, lq, `("is_active" = @`)
	require.Contains(t, lq, `("amount" >= @`)
	require.Contains(t, lq, `("created_at" >= @`)
	require.Contains(t, la, true)
	require.Contains(t, la, "10.50")
	// 无时区的过滤值按数据源时区（上海）解释，写给驱动的是同一墙上时间
	require.Contains(t, la, time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC))
}

func TestPage_FilterExprGroups(t *testing.T) {
	s := dialectSource(t, DriverPostgres)
	req := &paginationV1.PagingRequest{FilteringType: &paginationV1.PagingRequest_FilterExpr{FilterExpr: &paginationV1.FilterExpr{
		Type: paginationV1.ExprType_OR,
		Conditions: []*paginationV1.FilterCondition{
			{Field: "id", Op: paginationV1.Operator_IN, Values: []string{"a", "b"}},
			{Field: "amount", Op: paginationV1.Operator_BETWEEN, Values: []string{"1", "2.5"}},
		},
		Groups: []*paginationV1.FilterExpr{{
			Type:       paginationV1.ExprType_AND,
			Conditions: []*paginationV1.FilterCondition{{Field: "name", Op: paginationV1.Operator_IS_NULL}},
		}},
	}}}
	_, _, cq, ca := renderPage(t, s, req)
	require.Equal(t, `SELECT COUNT(*) FROM "ext_demo_contacts" WHERE (("id" IN ($1, $2)) OR (("amount" >= $3) AND ("amount" <= $4)) OR ("contact_name" IS NULL))`, cq)
	require.Equal(t, []any{"a", "b", "1", "2.5"}, ca)
}

func TestPage_RejectsBadInput(t *testing.T) {
	s := dialectSource(t, DriverMySQL)
	cases := map[string]*paginationV1.PagingRequest{
		"filter field not whitelisted": {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"password__contains":"x"}`}},
		"sort field not whitelisted":   {OrderBy: ptr(`["password"]`)},
		"injection in sort field":      {OrderBy: ptr(`["id; DROP TABLE x"]`)},
		"bool value":                   {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"is_active":"maybe"}`}},
		"decimal value":                {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"amount__gt":"1e10"}`}},
		"time value":                   {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"created_at__gt":"yesterday"}`}},
		"contains on non-text":         {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"amount__contains":"1"}`}},
		"unsupported operator":         {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"name__regex":"^a"}`}},
		"gt on bool":                   {FilteringType: &paginationV1.PagingRequest_Query{Query: `{"is_active__gt":"true"}`}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := s.Page(s.From(testTable.Name), testTable, req)
			require.Error(t, err)
			require.True(t, externalV1.IsBadRequest(err), "want BAD_REQUEST, got %v", err)
			require.EqualValues(t, 400, kerrors.FromError(err).Code)
		})
	}
}

func TestPage_EmptyContainsAddsNoCondition(t *testing.T) {
	s := dialectSource(t, DriverPostgres)
	_, _, cq, _ := renderPage(t, s, &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"name__contains":""}`},
	})
	require.Equal(t, `SELECT COUNT(*) FROM "ext_demo_contacts"`, cq)
}

func TestTimeConversion(t *testing.T) {
	s := dialectSource(t, DriverPostgres)
	instant := time.Date(2026, 3, 1, 0, 30, 0, 0, time.UTC) // 上海 08:30
	db := s.TimeToDB(instant)
	require.Equal(t, time.Date(2026, 3, 1, 8, 30, 0, 0, time.UTC), db)
	back := s.TimeFromDB(db)
	require.True(t, back.Equal(instant))
	require.Equal(t, "Asia/Shanghai", back.Location().String())
}

func TestNewSource_RejectsUnknownDriver(t *testing.T) {
	_, err := NewSource("x", "oracle", nil, Options{})
	require.Error(t, err)
}

func pagingQuery(q string) *paginationV1.PagingRequest {
	return &paginationV1.PagingRequest{FilteringType: &paginationV1.PagingRequest_Query{Query: q}}
}
