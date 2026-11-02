package server

import (
	"context"
	"errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
)

// ErrorMapping 把漏到传输层的非 kratos 错误统一映射成带 reason 的 kratos 错误。
//
// 仓储层很多路径（go-crud Repository.Get 等）把 ent 原始错误原样往上抛，
// kratos 的默认编码会把它们一律变成 500、reason 为空、message 是 ent 原文
// （"ent: dict_type not found"）：前端拿不到 reason 只能报"未知错误"，监控上是 5xx 噪声。
// 在这里集中处理，免得每个仓储各自判断：
//   - ent NotFound → 404 NOT_FOUND
//   - ent 约束冲突（唯一索引等）→ 409 CONFLICT
//   - 其他非 kratos 错误 → 500，响应不带原文，原始错误写日志
//
// 已经是 kratos 错误的（含被 %w 包住的）原样放行。
func ErrorMapping(log *bLogger.Helper) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			reply, err := handler(ctx, req)
			if err != nil {
				return reply, mapTransportError(ctx, log, err)
			}
			return reply, nil
		}
	}
}

func mapTransportError(ctx context.Context, log *bLogger.Helper, err error) error {
	var ke *kratosErrors.Error
	if errors.As(err, &ke) {
		return err
	}

	switch {
	case ent.IsNotFound(err):
		log.Warnf(ctx, "resource not found: %v", err)
		return adminV1.ErrorNotFound("resource not found")
	case ent.IsConstraintError(err):
		log.Warnf(ctx, "constraint violation: %v", err)
		return adminV1.ErrorConflict("conflicts with existing data")
	}

	log.Errorf(ctx, "unhandled error: %+v", err)
	return adminV1.ErrorInternalServerError("internal server error")
}
