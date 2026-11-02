package pagination

import (
	"entgo.io/ent/dialect/sql"

	"go-wind-admin/third_party/go-crud/pagination/paginator"
)

// OffsetPaginator 基于 Offset 的分页器。与 PagePaginator 一样被并发请求共享，不保存请求级状态。
type OffsetPaginator struct{}

func NewOffsetPaginator() *OffsetPaginator {
	return &OffsetPaginator{}
}

func (p *OffsetPaginator) BuildSelector(offset, limit int) func(*sql.Selector) {
	// 注意参数顺序：WithOffset(offset).WithLimit(limit)。
	// 此前两者写反（WithLimit(offset).WithOffset(limit)），导致 OFFSET/LIMIT 互换，
	// offset 分页全部返回错误的页窗口。
	impl := paginator.NewOffsetPaginatorWithDefault().
		WithOffset(offset).
		WithLimit(limit)
	off, lim := impl.Offset(), impl.Limit()

	return func(s *sql.Selector) {
		s.Offset(off).Limit(lim)
	}
}
