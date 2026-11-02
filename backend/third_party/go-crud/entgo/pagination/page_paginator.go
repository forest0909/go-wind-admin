package pagination

import (
	"entgo.io/ent/dialect/sql"
	"go-wind-admin/third_party/go-crud/pagination/paginator"
)

// PagePaginator 基于页码的分页器。
//
// 它挂在 Repository 单例上，被所有并发请求共享，所以不能保存任何请求级状态：
// BuildSelector 每次新建一个底层分页器，算出 offset/limit 局部值，闭包只捕获这两个值。
// 以前把页码写进共享字段、闭包在执行查询时才读，并发请求会互相改掉对方的 OFFSET/LIMIT。
type PagePaginator struct{}

func NewPagePaginator() *PagePaginator {
	return &PagePaginator{}
}

func (p *PagePaginator) BuildSelector(page, pageSize int) func(*sql.Selector) {
	impl := paginator.NewPagePaginatorWithDefault().
		WithPage(page).
		WithSize(pageSize)
	offset, limit := impl.Offset(), impl.Limit()

	return func(s *sql.Selector) {
		s.Offset(offset).Limit(limit)
	}
}
