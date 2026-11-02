package extdb

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	mssql "github.com/microsoft/go-mssqldb"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
)

// 返回给调用方的错误只带数据源名，不带连接串和驱动原始错误；原始错误在本包记日志。
// Errors returned to callers carry only the source name; raw driver errors are logged here.

// ErrNotConfigured 配置里没有这个数据源 / The source is not configured.
func ErrNotConfigured(name string) error {
	return externalV1.ErrorExternalSourceNotConfigured("external source %q is not configured", name)
}

// ErrReadOnly 只读数据源拒绝写 / Write rejected on a read-only source.
func ErrReadOnly(name string) error {
	return externalV1.ErrorExternalSourceReadOnly("external source %q is read-only", name)
}

// ErrUnavailable 外部库连不上 / The external database cannot be reached.
func ErrUnavailable(name string) error {
	return externalV1.ErrorExternalSourceUnavailable("external source %q is unavailable", name)
}

// ErrTimeout 超过 query_timeout / The statement exceeded query_timeout.
func ErrTimeout(name string) error {
	return externalV1.ErrorExternalSourceTimeout("external source %q query timed out", name)
}

// ErrBadRequest 过滤/排序/值不合法 / Invalid filter, sort field or value.
func ErrBadRequest(format string, args ...any) error {
	return externalV1.ErrorBadRequest(format, args...)
}

// ErrNotFound 记录不存在 / Record not found.
func ErrNotFound(name, what string) error {
	return externalV1.ErrorNotFound("external source %q: %s not found", name, what)
}

// buildError SQL 构造失败：是代码问题（例如方言不支持的表达式），按 500 处理。
func (s *Source) buildError(ctx context.Context, err error) error {
	s.log.Errorf(ctx, "external source %s: build sql failed: %v", s.name, err)
	return externalV1.ErrorInternalServerError("external source %q: build sql failed", s.name)
}

// wrap 把驱动错误归类成固定 reason。ctx 是本次语句带超时的 context，用来判断是否超时。
// write 为 true 时，数据库拒绝权限（只读账号）也归为只读拒绝。
func (s *Source) wrap(ctx context.Context, op string, write bool, err error) error {
	if err == nil {
		return nil
	}
	s.log.Errorf(ctx, "external source %s: %s failed: %v", s.name, op, err)
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ErrTimeout(s.name)
	case errors.Is(err, context.Canceled):
		// 调用方取消（客户端断开），原样返回。
		return err
	case isReadOnlyViolation(err, write):
		return ErrReadOnly(s.name)
	case isUnavailable(err):
		return ErrUnavailable(s.name)
	default:
		return externalV1.ErrorInternalServerError("external source %q: %s failed", s.name, op)
	}
}

// isReadOnlyViolation 数据库层的只读拒绝：PG/MySQL 只读事务里写，或账号没有写权限。
func isReadOnlyViolation(err error, write bool) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 25006 read_only_sql_transaction；42501 insufficient_privilege（只在写时算只读）
		return pgErr.Code == "25006" || (write && pgErr.Code == "42501")
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		// 1792 ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION；1142 ER_TABLEACCESS_DENIED_ERROR
		return myErr.Number == 1792 || (write && myErr.Number == 1142)
	}
	var msErr mssql.Error
	if errors.As(err, &msErr) {
		// 229 permission denied（SQL Server 靠只读账号做只读）
		return write && msErr.Number == 229
	}
	return false
}

// isUnavailable 连不上：网络错误、连接失效、认证失败、库不存在。
func isUnavailable(err error) bool {
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 08 connection_exception；28 invalid_authorization；3D000 invalid_catalog_name
		return strings.HasPrefix(pgErr.Code, "08") || strings.HasPrefix(pgErr.Code, "28") || pgErr.Code == "3D000"
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		// 1045 access denied；1049 unknown database
		return myErr.Number == 1045 || myErr.Number == 1049
	}
	var msErr mssql.Error
	if errors.As(err, &msErr) {
		// 18456 login failed；4060 cannot open database
		return msErr.Number == 18456 || msErr.Number == 4060
	}
	return false
}
