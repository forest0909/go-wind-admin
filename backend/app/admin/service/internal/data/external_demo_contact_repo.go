package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"go-wind-admin/third_party/go-utils/stringcase"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
	"go-wind-admin/pkg/extdb"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
)

// 外部数据模块的范例：数据在外部数据源 demo 的 ext_demo_contacts 表里，不走 ent。
// 建表脚本在 backend/sql/external_demo/，系统不会自动建表。写法规范见 docs/crud_module_guide/external.md。
// Example external-data module: rows live in table ext_demo_contacts of external source "demo".

// externalDemoSource 数据源名，对应配置 data.external_sources.demo
const externalDemoSource = "demo"

// demoContactTable 列白名单：API 字段名（snake_case）→ 外部列。只有这里登记的字段能过滤、排序。
// 客户的列名和 API 字段名不同时，只改 Column.Name。
var demoContactTable = &extdb.Table{
	Name: "ext_demo_contacts",
	PK:   "id",
	Columns: map[string]extdb.Column{
		"id":         {Name: "id", Type: extdb.TypeString},
		"name":       {Name: "name", Type: extdb.TypeString},
		"code":       {Name: "code", Type: extdb.TypeString},
		"is_active":  {Name: "is_active", Type: extdb.TypeBool},
		"amount":     {Name: "amount", Type: extdb.TypeDecimal},
		"remark":     {Name: "remark", Type: extdb.TypeString},
		"created_at": {Name: "created_at", Type: extdb.TypeTime},
		"updated_at": {Name: "updated_at", Type: extdb.TypeTime},
	},
}

// demoContactSelect SELECT 的列，顺序和 scanDemoContact 一致。
var demoContactSelect = []any{"id", "name", "code", "is_active", "amount", "remark", "created_at", "updated_at"}

// demoContactUpdatable update_mask 允许的字段 → 外部列。id、时间由系统维护，不在里面。
var demoContactUpdatable = map[string]string{
	"name":      "name",
	"code":      "code",
	"is_active": "is_active",
	"amount":    "amount",
	"remark":    "remark",
}

// ExternalDemoContactRepo 外部数据示例：联系人
type ExternalDemoContactRepo struct {
	log     *bLogger.Helper
	sources *extdb.Registry
}

func NewExternalDemoContactRepo(ctx *bootstrap.Context, sources *extdb.Registry) *ExternalDemoContactRepo {
	return &ExternalDemoContactRepo{
		log:     ctx.NewLoggerHelper("external-demo-contact/repo/admin-service"),
		sources: sources,
	}
}

// List 分页查询
func (r *ExternalDemoContactRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*externalV1.ListDemoContactResponse, error) {
	src, err := r.sources.Get(externalDemoSource)
	if err != nil {
		return nil, err
	}
	list, count, err := src.Page(src.From(demoContactTable.Name).Select(demoContactSelect...), demoContactTable, req)
	if err != nil {
		return nil, err
	}

	var total uint64
	if err = src.Query(ctx, count, func(rows *sql.Rows) error { return rows.Scan(&total) }); err != nil {
		return nil, err
	}
	items := make([]*externalV1.DemoContact, 0)
	if err = src.Query(ctx, list, func(rows *sql.Rows) error {
		item, err := scanDemoContact(src, rows)
		if err != nil {
			return err
		}
		items = append(items, item)
		return nil
	}); err != nil {
		return nil, err
	}
	return &externalV1.ListDemoContactResponse{Items: items, Total: total}, nil
}

// Get 按 ID 查询
func (r *ExternalDemoContactRepo) Get(ctx context.Context, id string) (*externalV1.DemoContact, error) {
	src, err := r.sources.Get(externalDemoSource)
	if err != nil {
		return nil, err
	}
	var item *externalV1.DemoContact
	err = src.Query(ctx,
		src.From(demoContactTable.Name).Select(demoContactSelect...).Where(goqu.C("id").Eq(id)),
		func(rows *sql.Rows) error {
			item, err = scanDemoContact(src, rows)
			return err
		})
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, extdb.ErrNotFound(externalDemoSource, "contact "+id)
	}
	return item, nil
}

// Create 新建，ID 由应用生成 UUID，返回新 ID
func (r *ExternalDemoContactRepo) Create(ctx context.Context, data *externalV1.DemoContact) (string, error) {
	src, err := r.sources.Get(externalDemoSource)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	now := src.TimeToDB(time.Now())
	_, err = src.Exec(ctx, src.Insert(demoContactTable.Name).Rows(goqu.Record{
		"id":         id,
		"name":       data.GetName(),
		"code":       nullable(data.Code),
		"is_active":  data.IsActive == nil || data.GetIsActive(), // 不传默认启用
		"amount":     nullable(data.Amount),
		"remark":     nullable(data.Remark),
		"created_at": now,
		"updated_at": now,
	}))
	if err != nil {
		return "", err
	}
	return id, nil
}

// Update 按 update_mask 更新；mask 里的字段值为空即写 NULL
func (r *ExternalDemoContactRepo) Update(ctx context.Context, id string, data *externalV1.DemoContact, mask *fieldmaskpb.FieldMask) error {
	src, err := r.sources.Get(externalDemoSource)
	if err != nil {
		return err
	}
	rec := goqu.Record{}
	for _, path := range mask.GetPaths() {
		field := stringcase.ToSnakeCase(path)
		if field == "id" {
			continue // 前端的 updateMask 末尾总会带 id（全仓惯例），主键不更新，忽略
		}
		col, ok := demoContactUpdatable[field]
		if !ok {
			return extdb.ErrBadRequest("field %q cannot be updated", path)
		}
		switch field {
		case "name":
			rec[col] = data.GetName()
		case "code":
			rec[col] = nullable(data.Code)
		case "is_active":
			rec[col] = data.GetIsActive()
		case "amount":
			rec[col] = nullable(data.Amount)
		case "remark":
			rec[col] = nullable(data.Remark)
		}
	}
	if len(rec) == 0 {
		return extdb.ErrBadRequest("update_mask has no updatable field")
	}
	rec["updated_at"] = src.TimeToDB(time.Now())

	n, err := src.Exec(ctx, src.Update(demoContactTable.Name).Set(rec).Where(goqu.C("id").Eq(id)))
	if err != nil {
		return err
	}
	if n == 0 {
		return extdb.ErrNotFound(externalDemoSource, "contact "+id)
	}
	return nil
}

// Delete 按 ID 删除
func (r *ExternalDemoContactRepo) Delete(ctx context.Context, id string) error {
	src, err := r.sources.Get(externalDemoSource)
	if err != nil {
		return err
	}
	n, err := src.Exec(ctx, src.Delete(demoContactTable.Name).Where(goqu.C("id").Eq(id)))
	if err != nil {
		return err
	}
	if n == 0 {
		return extdb.ErrNotFound(externalDemoSource, "contact "+id)
	}
	return nil
}

// scanDemoContact 扫一行。可空列用 sql.Null*；金额按字符串读，不经过 float；
// 时间列是无时区的，按数据源时区解释。
func scanDemoContact(src *extdb.Source, rows *sql.Rows) (*externalV1.DemoContact, error) {
	var (
		id, name             string
		code, amount, remark sql.NullString
		isActive             bool
		createdAt, updatedAt sql.NullTime
	)
	if err := rows.Scan(&id, &name, &code, &isActive, &amount, &remark, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return &externalV1.DemoContact{
		Id:        &id,
		Name:      &name,
		Code:      nullStringPtr(code),
		IsActive:  &isActive,
		Amount:    nullStringPtr(amount),
		Remark:    nullStringPtr(remark),
		CreatedAt: nullTimeToPb(src, createdAt),
		UpdatedAt: nullTimeToPb(src, updatedAt),
	}, nil
}

func nullable[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullTimeToPb(src *extdb.Source, v sql.NullTime) *timestamppb.Timestamp {
	if !v.Valid {
		return nil
	}
	return timestamppb.New(src.TimeFromDB(v.Time))
}
