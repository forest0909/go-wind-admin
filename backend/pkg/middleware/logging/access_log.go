package logging

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/go-kratos/kratos/v2/transport/http/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// kratosRedacter 是 Kratos logging.Server 认的接口：Redact 必须返回脱敏后的字符串。
// 本仓 protoc-gen-redact 生成的是原地清空、没有返回值的 Redact()，对不上这个接口，
// 所以不能直接用 Kratos 那份中间件。
type kratosRedacter interface {
	Redact() string
}

// inPlaceRedacter 是本仓生成代码实现的脱敏：原地清空字段，不返回字符串。
type inPlaceRedacter interface {
	Redact()
}

// sensitiveProtoFieldNames 是字段名兜底名单。
// 注解会生成原地 Redact()，但以后新增口令字段的人可能忘了写 (redact.value)。
// 名单按 proto 字段名（不是 json_name）匹配 string 字段，避免这种遗漏把口令打进访问日志。
var sensitiveProtoFieldNames = map[protoreflect.Name]struct{}{
	"password":       {},
	"old_password":   {},
	"new_password":   {},
	"credential":     {},
	"old_credential": {},
	"new_credential": {},
	"client_secret":  {},
	"refresh_token":  {},
	"access_token":   {},
	"secret":         {},
	"token":          {},
}

// AccessLogServer 是访问日志中间件，替换 Kratos logging.Server。
// 字段名、级别、latency、code、reason 与 Kratos 原版一致。
// 记录参数时不改原始 req：handler 还要用里面的口令。
func AccessLogServer(logger log.Logger) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (reply any, err error) {
			var (
				code      int32
				reason    string
				kind      string
				operation string
			)

			code = int32(status.FromGRPCCode(codes.OK))

			startTime := time.Now()
			if info, ok := transport.FromServerContext(ctx); ok {
				kind = info.Kind().String()
				operation = info.Operation()
			}
			reply, err = handler(ctx, req)
			if se := errors.FromError(err); se != nil {
				code = se.Code
				reason = se.Reason
			}
			level, stack := extractAccessLogError(err)
			log.NewHelper(log.WithContext(ctx, logger)).Log(level,
				"kind", "server",
				"component", kind,
				"operation", operation,
				"args", FormatArgs(req),
				"code", code,
				"reason", reason,
				"stack", stack,
				"latency", time.Since(startTime).Seconds(),
			)
			return
		}
	}
}

// FormatArgs 把请求格式化成可写入日志的字符串。
// proto 消息先克隆，在克隆上做原地 Redact 和字段名兜底，再输出克隆。
// 其它类型保持 Kratos extractArgs 的顺序：Redact() string，其次 String()，最后 %+v。
func FormatArgs(req any) string {
	if msg, ok := req.(proto.Message); ok && msg != nil {
		cloned := proto.Clone(msg)
		if cloned != nil && cloned.ProtoReflect().IsValid() {
			if redacter, ok := cloned.(inPlaceRedacter); ok {
				redacter.Redact()
			}
			redactSensitiveStrings(cloned.ProtoReflect())
			if stringer, ok := cloned.(fmt.Stringer); ok {
				return stringer.String()
			}
			return fmt.Sprintf("%+v", cloned)
		}
	}
	if redacter, ok := req.(kratosRedacter); ok {
		return redacter.Redact()
	}
	if stringer, ok := req.(fmt.Stringer); ok {
		return stringer.String()
	}
	return fmt.Sprintf("%+v", req)
}

func extractAccessLogError(err error) (log.Level, string) {
	if err != nil {
		return log.LevelError, fmt.Sprintf("%+v", err)
	}
	return log.LevelInfo, ""
}

func sensitiveProtoField(name protoreflect.Name) bool {
	_, ok := sensitiveProtoFieldNames[name]
	return ok
}

// redactSensitiveStrings 递归清空敏感 string 字段，覆盖嵌套消息、repeated 和 map 的值。
func redactSensitiveStrings(m protoreflect.Message) {
	if !m.IsValid() {
		return
	}
	var clear []protoreflect.FieldDescriptor
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			redactSensitiveMap(fd, v.Map())
		case fd.IsList():
			redactSensitiveList(fd, v.List())
		case fd.Message() != nil:
			redactSensitiveStrings(v.Message())
		case fd.Kind() == protoreflect.StringKind && sensitiveProtoField(fd.Name()):
			clear = append(clear, fd)
		}
		return true
	})
	for _, fd := range clear {
		m.Clear(fd)
	}
}

func redactSensitiveList(fd protoreflect.FieldDescriptor, list protoreflect.List) {
	if fd.Message() != nil {
		for i := 0; i < list.Len(); i++ {
			redactSensitiveStrings(list.Get(i).Message())
		}
		return
	}
	if fd.Kind() == protoreflect.StringKind && sensitiveProtoField(fd.Name()) {
		for i := 0; i < list.Len(); i++ {
			list.Set(i, protoreflect.ValueOfString(""))
		}
	}
}

func redactSensitiveMap(fd protoreflect.FieldDescriptor, mp protoreflect.Map) {
	valueDesc := fd.MapValue()
	if valueDesc.Message() != nil {
		mp.Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool {
			redactSensitiveStrings(v.Message())
			return true
		})
		return
	}
	if valueDesc.Kind() != protoreflect.StringKind || !sensitiveProtoField(fd.Name()) {
		return
	}
	var keys []protoreflect.MapKey
	mp.Range(func(k protoreflect.MapKey, _ protoreflect.Value) bool {
		keys = append(keys, k)
		return true
	})
	for _, k := range keys {
		mp.Set(k, protoreflect.ValueOfString(""))
	}
}
