// Package extdb 外部数据源：客户已有的业务库（PostgreSQL / MySQL / SQL Server）。
// Package extdb provides access to customer-owned external databases.
//
// 外部库不走 ent：用 database/sql + 驱动，SQL 由 goqu 按方言构造（全部用占位符参数，不拼字面量）。
// 本包只做查询和写入，绝不建表、改表；ent 自动迁移和升级步骤只作用于主库。
// 规范和完整例子见 docs/crud_module_guide/external.md，范例模块是 internal/data/external_demo_contact_repo.go。
package extdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	_ "github.com/doug-martin/goqu/v9/dialect/sqlite3"
	_ "github.com/doug-martin/goqu/v9/dialect/sqlserver"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// 配置里 driver 的取值 / Values of the `driver` config key.
const (
	DriverPostgres  = "postgres"
	DriverMySQL     = "mysql"
	DriverSQLServer = "sqlserver"

	// driverSQLite 只给单元测试用（NewSource 可传），配置里不接受。
	driverSQLite = "sqlite3"
)

// Builder 能生成 SQL 和参数的对象：goqu 的各种 Dataset 都满足。
// Builder is anything that renders SQL with arguments; every goqu dataset satisfies it.
type Builder interface {
	ToSQL() (string, []any, error)
}

// Options 构造 Source 的参数 / Options for NewSource.
type Options struct {
	ReadOnly     bool
	QueryTimeout time.Duration
	Location     *time.Location // 外部库无时区时间字段的解释时区，nil 为 UTC
	Required     bool
	Logger       *bLogger.Helper
}

// Source 一个命名的外部数据源 / A named external data source.
type Source struct {
	name     string
	driver   string
	db       *sql.DB
	dialect  goqu.DialectWrapper
	readOnly bool
	timeout  time.Duration
	loc      *time.Location
	required bool
	log      *bLogger.Helper
}

// NewSource 用已打开的连接池构造数据源。生产代码走 NewRegistry（按配置建池），这里给测试和 NewRegistry 共用。
// NewSource wraps an opened pool. Production code goes through NewRegistry.
func NewSource(name, driver string, db *sql.DB, opts Options) (*Source, error) {
	var dialect string
	switch driver {
	case DriverPostgres, DriverMySQL, DriverSQLServer, driverSQLite:
		dialect = driver
	default:
		return nil, fmt.Errorf("external source %q: unsupported driver %q (want postgres, mysql or sqlserver)", name, driver)
	}
	if opts.QueryTimeout <= 0 {
		opts.QueryTimeout = DefaultQueryTimeout
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Logger == nil {
		opts.Logger = bLogger.NewHelper(bLogger.NewStdLogger())
	}
	return &Source{
		name:     name,
		driver:   driver,
		db:       db,
		dialect:  goqu.Dialect(dialect),
		readOnly: opts.ReadOnly,
		timeout:  opts.QueryTimeout,
		loc:      opts.Location,
		required: opts.Required,
		log:      opts.Logger,
	}, nil
}

// Name 数据源名 / Source name.
func (s *Source) Name() string { return s.name }

// Driver 驱动名：postgres、mysql、sqlserver / Driver name.
func (s *Source) Driver() string { return s.driver }

// ReadOnly 是否只读 / Whether writes are rejected.
func (s *Source) ReadOnly() bool { return s.readOnly }

// Location 无时区时间字段的解释时区 / Time zone for naive time columns.
func (s *Source) Location() *time.Location { return s.loc }

// From 构造 SELECT（参数化）/ Starts a parameterized SELECT.
func (s *Source) From(table string) *goqu.SelectDataset {
	return s.dialect.From(table).Prepared(true)
}

// Insert 构造 INSERT（参数化）/ Starts a parameterized INSERT.
func (s *Source) Insert(table string) *goqu.InsertDataset {
	return s.dialect.Insert(table).Prepared(true)
}

// Update 构造 UPDATE（参数化）/ Starts a parameterized UPDATE.
func (s *Source) Update(table string) *goqu.UpdateDataset {
	return s.dialect.Update(table).Prepared(true)
}

// Delete 构造 DELETE（参数化）/ Starts a parameterized DELETE.
func (s *Source) Delete(table string) *goqu.DeleteDataset {
	return s.dialect.Delete(table).Prepared(true)
}

// Ping 探测连通性（/readyz 用），套 query_timeout。
// Ping checks connectivity within query_timeout.
func (s *Source) Ping(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.db.PingContext(cctx)
}

// Query 执行查询，每行调用一次 scan。只读源在 PG/MySQL 上用只读事务执行（第二层保护）。
// Query runs a SELECT and calls scan for each row. On read-only PG/MySQL sources the
// statement runs inside a READ ONLY transaction as a second line of defence.
func (s *Source) Query(ctx context.Context, b Builder, scan func(*sql.Rows) error) error {
	query, args, err := b.ToSQL()
	if err != nil {
		return s.buildError(ctx, err)
	}
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	if s.readOnly && s.supportsReadOnlyTx() {
		tx, err := s.db.BeginTx(cctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return s.wrap(cctx, "query", false, err)
		}
		err = queryRows(cctx, tx, query, args, scan)
		// 只读事务里没有要提交的东西，回滚即可；回滚失败不影响已读到的结果，但要记下来。
		if rbErr := tx.Rollback(); rbErr != nil && err == nil {
			s.log.Warnf(ctx, "external source %s: rollback read-only tx failed: %v", s.name, rbErr)
		}
		return s.wrap(cctx, "query", false, err)
	}
	return s.wrap(cctx, "query", false, queryRows(cctx, s.db, query, args, scan))
}

// Exec 执行一条写语句，返回影响行数。只读源直接拒绝。
// Exec runs one write statement and returns the affected row count. Rejected on read-only sources.
func (s *Source) Exec(ctx context.Context, b Builder) (int64, error) {
	if err := s.checkWritable(ctx); err != nil {
		return 0, err
	}
	query, args, err := b.ToSQL()
	if err != nil {
		return 0, s.buildError(ctx, err)
	}
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	n, err := execStmt(cctx, s.db, query, args)
	return n, s.wrap(cctx, "exec", true, err)
}

// Tx 在本数据源内开一个读写事务：fn 返回 nil 提交，返回错误或 panic 回滚。
// 每条语句各自套 query_timeout。不支持跨数据源（包括主库）的事务。
// Tx runs fn in a read-write transaction on this source only; each statement gets query_timeout.
func (s *Source) Tx(ctx context.Context, fn func(tx *Tx) error) (err error) {
	if err := s.checkWritable(ctx); err != nil {
		return err
	}
	sqlTx, cancelTx, err := s.beginTx(ctx)
	if err != nil {
		return err
	}
	defer cancelTx()
	defer func() {
		if p := recover(); p != nil {
			if rbErr := sqlTx.Rollback(); rbErr != nil {
				s.log.Errorf(ctx, "external source %s: rollback after panic failed: %v", s.name, rbErr)
			}
			panic(p)
		}
	}()
	if err := fn(&Tx{src: s, tx: sqlTx}); err != nil {
		if rbErr := sqlTx.Rollback(); rbErr != nil {
			s.log.Errorf(ctx, "external source %s: rollback failed: %v", s.name, rbErr)
		}
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return s.wrap(ctx, "commit", true, err)
	}
	return nil
}

// beginTx 开事务：取连接和 BEGIN 这一段也受 query_timeout 约束（池满时不会无限等），超时返回
// EXTERNAL_SOURCE_TIMEOUT。BeginTx 的 ctx 管着整个事务（ctx 结束 database/sql 就回滚），
// 所以不能用带期限的 ctx：用可取消的 ctx 加定时器，BEGIN 成功后停掉定时器，事务结束时再 cancel。
// 之后每条语句的超时由 Tx.Query / Tx.Exec 各自套。
func (s *Source) beginTx(ctx context.Context) (*sql.Tx, context.CancelFunc, error) {
	txCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(s.timeout, cancel)
	sqlTx, err := s.db.BeginTx(txCtx, nil)
	if !timer.Stop() {
		// 定时器已经触发：BEGIN 超时，或者刚成功就被取消（事务已随 ctx 回滚）。
		if err == nil {
			if rbErr := sqlTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				s.log.Errorf(ctx, "external source %s: rollback after begin timeout failed: %v", s.name, rbErr)
			}
		} else {
			s.log.Errorf(ctx, "external source %s: begin failed: %v", s.name, err)
		}
		cancel()
		s.log.Errorf(ctx, "external source %s: begin transaction timed out after %s", s.name, s.timeout)
		return nil, nil, ErrTimeout(s.name)
	}
	if err != nil {
		cancel()
		return nil, nil, s.wrap(txCtx, "begin", true, err)
	}
	return sqlTx, cancel, nil
}

// Tx 数据源内的事务句柄 / A transaction on one source.
type Tx struct {
	src *Source
	tx  *sql.Tx
}

// Query 同 Source.Query，在事务内执行 / Same as Source.Query inside the transaction.
func (t *Tx) Query(ctx context.Context, b Builder, scan func(*sql.Rows) error) error {
	query, args, err := b.ToSQL()
	if err != nil {
		return t.src.buildError(ctx, err)
	}
	cctx, cancel := context.WithTimeout(ctx, t.src.timeout)
	defer cancel()
	return t.src.wrap(cctx, "query", true, queryRows(cctx, t.tx, query, args, scan))
}

// Exec 同 Source.Exec，在事务内执行 / Same as Source.Exec inside the transaction.
func (t *Tx) Exec(ctx context.Context, b Builder) (int64, error) {
	query, args, err := b.ToSQL()
	if err != nil {
		return 0, t.src.buildError(ctx, err)
	}
	cctx, cancel := context.WithTimeout(ctx, t.src.timeout)
	defer cancel()
	n, err := execStmt(cctx, t.tx, query, args)
	return n, t.src.wrap(cctx, "exec", true, err)
}

// TimeToDB 把时间换成写进无时区列（TypeTime）的值：按数据源时区取墙上时间，标成 UTC 交给驱动
// （三种驱动对 UTC 时间都原样写墙上时间）。带时区的列（TypeTimeTZ：timestamptz、datetimeoffset）直接传原值，不要调它。
// TimeToDB converts t for a naive (zone-less) column: wall clock in the source zone, labelled UTC.
func (s *Source) TimeToDB(t time.Time) time.Time {
	w := t.In(s.loc)
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), time.UTC)
}

// TimeFromDB 把从无时区列（TypeTime）读出的值（驱动标成 UTC 的墙上时间）按数据源时区解释。
// 带时区的列（TypeTimeTZ）读出来就是正确的瞬间，不要调它。
// TimeFromDB interprets a naive column value (wall clock labelled UTC by the driver) in the source zone.
func (s *Source) TimeFromDB(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), s.loc)
}

func (s *Source) supportsReadOnlyTx() bool {
	// SQL Server 没有只读事务，靠客户给只读账号；SQLite 只在单元测试里出现。
	return s.driver == DriverPostgres || s.driver == DriverMySQL
}

func (s *Source) checkWritable(ctx context.Context) error {
	if s.readOnly {
		s.log.Warnf(ctx, "external source %s is read-only, write rejected", s.name)
		return ErrReadOnly(s.name)
	}
	return nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func queryRows(ctx context.Context, q queryer, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func execStmt(ctx context.Context, q queryer, query string, args []any) (int64, error) {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
