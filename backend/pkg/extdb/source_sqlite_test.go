package extdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
)

// rawSQL 测试里直接写的 SQL / Raw SQL for tests.
type rawSQL struct {
	query string
	args  []any
}

func (r rawSQL) ToSQL() (string, []any, error) { return r.query, r.args, nil }

func openSQLite(t *testing.T, opts Options) *Source {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO kv VALUES ('a', 'Apple'), ('b', 'banana'), ('c', '100%_x')`)
	require.NoError(t, err)
	s, err := NewSource("demo", driverSQLite, db, opts)
	require.NoError(t, err)
	return s
}

func countKV(t *testing.T, s *Source) int {
	t.Helper()
	var n int
	require.NoError(t, s.Query(context.Background(), s.From("kv").Select(goqu.COUNT(goqu.Star())), func(r *sql.Rows) error {
		return r.Scan(&n)
	}))
	return n
}

func TestRegistry_NotConfigured(t *testing.T) {
	s := openSQLite(t, Options{})
	r := NewStaticRegistry(s)
	got, err := r.Get("demo")
	require.NoError(t, err)
	require.Same(t, s, got)

	_, err = r.Get("erp")
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)

	var nilReg *Registry
	_, err = nilReg.Get("demo")
	require.True(t, externalV1.IsExternalSourceNotConfigured(err))
}

func TestRegistry_RequiredSorted(t *testing.T) {
	a, err := NewSource("b", DriverPostgres, nil, Options{Required: true})
	require.NoError(t, err)
	b, err := NewSource("a", DriverPostgres, nil, Options{Required: true})
	require.NoError(t, err)
	c, err := NewSource("c", DriverPostgres, nil, Options{})
	require.NoError(t, err)
	got := NewStaticRegistry(a, b, c).Required()
	require.Len(t, got, 2)
	require.Equal(t, "a", got[0].Name())
	require.Equal(t, "b", got[1].Name())
}

func TestSource_ReadOnlyRejectsWrites(t *testing.T) {
	s := openSQLite(t, Options{ReadOnly: true})
	ctx := context.Background()

	_, err := s.Exec(ctx, s.Insert("kv").Rows(goqu.Record{"k": "d", "v": "x"}))
	require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)

	called := false
	err = s.Tx(ctx, func(tx *Tx) error { called = true; return nil })
	require.True(t, externalV1.IsExternalSourceReadOnly(err), "got %v", err)
	require.False(t, called)

	require.Equal(t, 3, countKV(t, s), "reads still work")
}

func TestSource_ContainsMatchesLiterally(t *testing.T) {
	s := openSQLite(t, Options{})
	tbl := &Table{Name: "kv", PK: "k", Columns: map[string]Column{
		"k": {Name: "k", Type: TypeString}, "v": {Name: "v", Type: TypeString},
	}}
	keys := func(query string) []string {
		list, _, err := s.Page(s.From("kv").Select("k"), tbl, pagingQuery(query))
		require.NoError(t, err)
		var out []string
		require.NoError(t, s.Query(context.Background(), list, func(r *sql.Rows) error {
			var k string
			out = append(out, k)
			return r.Scan(&out[len(out)-1])
		}))
		return out
	}
	require.Equal(t, []string{"a"}, keys(`{"v__contains":"apple"}`), "case-insensitive")
	require.Equal(t, []string{"c"}, keys(`{"v__contains":"%_"}`), "wildcards are literal")
	require.Empty(t, keys(`{"v__contains":"0_x"}`))
	require.Equal(t, []string{"a", "b", "c"}, keys(`{}`))
}

func TestSource_TxCommitAndRollback(t *testing.T) {
	s := openSQLite(t, Options{})
	ctx := context.Background()

	err := s.Tx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, s.Insert("kv").Rows(goqu.Record{"k": "d", "v": "x"})); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, s.Update("kv").Set(goqu.Record{"v": "y"}).Where(goqu.C("k").Eq("a")))
		return err
	})
	require.NoError(t, err)
	require.Equal(t, 4, countKV(t, s))

	boom := errors.New("boom")
	err = s.Tx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, s.Delete("kv")); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 4, countKV(t, s), "rolled back")
}

func TestSource_QueryTimeout(t *testing.T) {
	s := openSQLite(t, Options{QueryTimeout: 50 * time.Millisecond})
	slow := rawSQL{query: `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT max(x) FROM c`}
	start := time.Now()
	err := s.Query(context.Background(), slow, func(r *sql.Rows) error { return nil })
	require.True(t, externalV1.IsExternalSourceTimeout(err), "got %v", err)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestSource_OtherErrorsAreInternal(t *testing.T) {
	s := openSQLite(t, Options{})
	err := s.Query(context.Background(), rawSQL{query: `SELECT nope FROM kv`}, func(r *sql.Rows) error { return nil })
	require.True(t, externalV1.IsInternalServerError(err), "got %v", err)
	require.NotContains(t, err.Error(), "nope", "driver error text stays in the log")
}
