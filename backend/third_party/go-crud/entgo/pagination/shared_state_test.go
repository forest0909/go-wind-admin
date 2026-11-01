package pagination

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"entgo.io/ent/dialect/sql"
)

func applySQL(sel func(*sql.Selector)) string {
	s := sql.Dialect("postgres").Select("*").From(sql.Table("t"))
	sel(s)
	q, _ := s.Query()
	return q
}

// 同一个分页器先后给两个请求建闭包，先建的闭包必须保持自己的页窗口。
// 修复前 A 的闭包在执行时读共享字段，拿到的是 B 的 LIMIT 20 OFFSET 80。
func TestPagePaginator_ClosureKeepsOwnWindow(t *testing.T) {
	p := NewPagePaginator()
	selA := p.BuildSelector(1, 10)
	_ = p.BuildSelector(5, 20)
	q := applySQL(selA)
	if !strings.Contains(q, "LIMIT 10") || !strings.Contains(q, "OFFSET 0") {
		t.Fatalf("request A expected LIMIT 10 OFFSET 0, got %q", q)
	}
}

func TestOffsetPaginator_ClosureKeepsOwnWindow(t *testing.T) {
	p := NewOffsetPaginator()
	selA := p.BuildSelector(0, 10)
	_ = p.BuildSelector(80, 20)
	q := applySQL(selA)
	if !strings.Contains(q, "LIMIT 10") || strings.Contains(q, "OFFSET 80") {
		t.Fatalf("request A expected LIMIT 10 without OFFSET 80, got %q", q)
	}
}

func TestTokenPaginator_ClosureKeepsOwnSize(t *testing.T) {
	p := NewTokenPaginator()
	selA := p.BuildSelector("", 10)
	_ = p.BuildSelector("", 20)
	if q := applySQL(selA); !strings.Contains(q, "LIMIT 10") {
		t.Fatalf("request A expected LIMIT 10, got %q", q)
	}
}

// 并发构造并执行闭包：配合 go test -race，修复前报 DATA RACE（共享 pagePaginator.page）。
func TestPaginators_ConcurrentBuildNoRace(t *testing.T) {
	pp, op, tp := NewPagePaginator(), NewOffsetPaginator(), NewTokenPaginator()
	var wg sync.WaitGroup
	errs := make(chan string, 300)
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			want := fmt.Sprintf("OFFSET %d", (n-1)*10)
			if q := applySQL(pp.BuildSelector(n, 10)); !strings.Contains(q, want) {
				errs <- fmt.Sprintf("page %d: want %s, got %q", n, want, q)
			}
			_ = applySQL(op.BuildSelector(n, 10))
			_ = applySQL(tp.BuildSelector("", n))
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
