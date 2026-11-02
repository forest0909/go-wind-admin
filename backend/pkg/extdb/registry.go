package extdb

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// 配置缺省值 / Config defaults.
const (
	DefaultMaxOpenConns    = 10
	DefaultMaxIdleConns    = 2
	DefaultConnMaxLifetime = 30 * time.Minute
	DefaultQueryTimeout    = 5 * time.Second
)

// Registry 按名字取数据源 / Looks up sources by name.
type Registry struct {
	sources map[string]*Source
}

// NewRegistry 按配置给每个外部数据源建连接池。
// 配置写错（driver 不认识、时区不认识、DSN 解析不了）启动失败；连不上不影响启动，只记日志，
// required 的源由 /readyz 报告。
// NewRegistry opens one pool per configured source. Bad config fails startup; an unreachable
// database is only logged (required sources then fail /readyz).
func NewRegistry(ctx context.Context, logger *bLogger.Helper, cfgs map[string]*conf.Data_ExternalSource) (*Registry, func(), error) {
	r := &Registry{sources: make(map[string]*Source, len(cfgs))}
	cleanup := func() {
		for name, s := range r.sources {
			if err := s.db.Close(); err != nil {
				logger.Errorf(ctx, "external source %s: close failed: %v", name, err)
			}
		}
	}
	for _, name := range sortedNames(cfgs) {
		src, err := openSource(name, cfgs[name], logger)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		r.sources[name] = src
		if err := src.Ping(ctx); err != nil {
			logger.Errorf(ctx, "external source %s (%s, required=%v) is not reachable at startup: %v",
				name, src.driver, src.required, err)
		} else {
			logger.Infof(ctx, "external source %s (%s, read_only=%v) connected", name, src.driver, src.readOnly)
		}
	}
	return r, cleanup, nil
}

// NewStaticRegistry 用现成的数据源组注册表，给测试用。
// NewStaticRegistry builds a registry from ready sources (tests).
func NewStaticRegistry(sources ...*Source) *Registry {
	r := &Registry{sources: make(map[string]*Source, len(sources))}
	for _, s := range sources {
		r.sources[s.name] = s
	}
	return r
}

// Get 取数据源；没配置时返回 EXTERNAL_SOURCE_NOT_CONFIGURED。
// Get returns the named source, or EXTERNAL_SOURCE_NOT_CONFIGURED.
func (r *Registry) Get(name string) (*Source, error) {
	if r != nil {
		if s, ok := r.sources[name]; ok {
			return s, nil
		}
	}
	return nil, ErrNotConfigured(name)
}

// Required 参与 /readyz 的数据源，按名字排序。
// Required returns the sources that take part in /readyz, sorted by name.
func (r *Registry) Required() []*Source {
	var out []*Source
	for _, name := range sortedNames(r.sources) {
		if s := r.sources[name]; s.required {
			out = append(out, s)
		}
	}
	return out
}

func openSource(name string, c *conf.Data_ExternalSource, logger *bLogger.Helper) (*Source, error) {
	if c == nil {
		return nil, fmt.Errorf("external source %q: empty config", name)
	}
	loc := time.UTC
	if c.GetTimezone() != "" {
		l, err := time.LoadLocation(c.GetTimezone())
		if err != nil {
			return nil, fmt.Errorf("external source %q: bad timezone %q: %w", name, c.GetTimezone(), err)
		}
		loc = l
	}

	var sqlDriver, dsn string
	switch c.GetDriver() {
	case DriverPostgres:
		sqlDriver, dsn = "pgx", c.GetDsn()
	case DriverMySQL:
		// 时间按 TimeToDB/TimeFromDB 约定处理：驱动必须解析 DATETIME，并且不做时区换算（loc=UTC）。
		mc, err := mysql.ParseDSN(c.GetDsn())
		if err != nil {
			// 解析错误里可能带 DSN 片段，不往外带，只说哪个源。
			return nil, fmt.Errorf("external source %q: invalid mysql dsn", name)
		}
		mc.ParseTime = true
		mc.Loc = time.UTC
		sqlDriver, dsn = "mysql", mc.FormatDSN()
	case DriverSQLServer:
		sqlDriver, dsn = "sqlserver", c.GetDsn()
	default:
		return nil, fmt.Errorf("external source %q: unsupported driver %q (want postgres, mysql or sqlserver)", name, c.GetDriver())
	}

	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("external source %q: open failed", name)
	}
	db.SetMaxOpenConns(orDefault(int(c.GetMaxOpenConns()), DefaultMaxOpenConns))
	db.SetMaxIdleConns(orDefault(int(c.GetMaxIdleConns()), DefaultMaxIdleConns))
	lifetime := DefaultConnMaxLifetime
	if c.GetConnMaxLifetime() != nil {
		lifetime = c.GetConnMaxLifetime().AsDuration()
	}
	db.SetConnMaxLifetime(lifetime)

	timeout := DefaultQueryTimeout
	if c.GetQueryTimeout() != nil && c.GetQueryTimeout().AsDuration() > 0 {
		timeout = c.GetQueryTimeout().AsDuration()
	}

	readOnly := true // 不写默认只读
	if c.ReadOnly != nil {
		readOnly = c.GetReadOnly()
	}

	return NewSource(name, c.GetDriver(), db, Options{
		ReadOnly:     readOnly,
		QueryTimeout: timeout,
		Location:     loc,
		Required:     c.GetRequired(),
		Logger:       logger,
	})
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
