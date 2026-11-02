package pagination

import (
	"entgo.io/ent/dialect/sql"
	"go-wind-admin/third_party/go-crud/pagination"

	"go-wind-admin/third_party/go-crud/pagination/paginator"
)

// TokenPaginator 基于 Token 的分页器。与 PagePaginator 一样被并发请求共享，不保存请求级状态。
type TokenPaginator struct{}

func NewTokenPaginator() *TokenPaginator {
	return &TokenPaginator{}
}

func (p *TokenPaginator) BuildSelector(token string, pageSize int) func(*sql.Selector) {
	size := paginator.NewTokenPaginatorWithDefault().
		WithToken(token).
		WithSize(pageSize).
		Size()

	// 无 token 或解码失败时只应用 pageSize
	if token == "" {
		return func(s *sql.Selector) {
			s.Limit(size)
		}
	}

	lastID, ok := pagination.VerifyAndDecode(token, pagination.TokenSecret())
	if !ok {
		return func(s *sql.Selector) {
			s.Limit(size)
		}
	}

	return func(s *sql.Selector) {
		s.Where(sql.GT("id", lastID))
		s.Limit(size)
	}
}
