package logging

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

// captureLogger 记下 Log 收到的级别和键值，供断言访问日志内容。
type captureLogger struct {
	level   log.Level
	keyvals []any
	calls   int
}

func (c *captureLogger) Log(level log.Level, keyvals ...any) error {
	c.calls++
	c.level = level
	c.keyvals = append([]any(nil), keyvals...)
	return nil
}

func kv(t *testing.T, keyvals []any, key string) any {
	t.Helper()
	for i := 0; i+1 < len(keyvals); i += 2 {
		name, ok := keyvals[i].(string)
		if ok && name == key {
			return keyvals[i+1]
		}
	}
	t.Fatalf("日志缺少字段 %s：%v", key, keyvals)
	return nil
}

func runAccessLog(t *testing.T, req any) (string, any) {
	t.Helper()
	logger := &captureLogger{}
	var seen any
	mw := AccessLogServer(logger)
	ctx := transport.NewServerContext(context.Background(), &fakeTransporter{op: "/test.AccessLog/Call"})
	_, err := mw(func(_ context.Context, in any) (any, error) {
		seen = in
		return "ok", nil
	})(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, logger.calls)
	require.Equal(t, log.LevelInfo, logger.level)
	require.Equal(t, []any{
		"kind", "component", "operation", "args", "code", "reason", "stack", "latency",
	}, keyNames(logger.keyvals))
	require.Equal(t, "server", kv(t, logger.keyvals, "kind"))
	require.Equal(t, "http", kv(t, logger.keyvals, "component"))
	require.Equal(t, "/test.AccessLog/Call", kv(t, logger.keyvals, "operation"))
	require.Equal(t, int32(200), kv(t, logger.keyvals, "code"))
	require.Equal(t, "", kv(t, logger.keyvals, "reason"))
	require.Equal(t, "", kv(t, logger.keyvals, "stack"))
	latency, ok := kv(t, logger.keyvals, "latency").(float64)
	require.True(t, ok, "latency 应为秒，类型 float64")
	require.GreaterOrEqual(t, latency, 0.0)
	args, ok := kv(t, logger.keyvals, "args").(string)
	require.True(t, ok)
	return args, seen
}

func keyNames(keyvals []any) []any {
	names := make([]any, 0, len(keyvals)/2)
	for i := 0; i+1 < len(keyvals); i += 2 {
		names = append(names, keyvals[i])
	}
	return names
}

func TestAccessLogServer_RedactsSensitiveRequests(t *testing.T) {
	login := &authenticationV1.LoginRequest{
		Identifier:   &authenticationV1.LoginRequest_Username{Username: "alice"},
		Password:     trans.Ptr("pw-Login-9f3a"),
		ClientSecret: trans.Ptr("cs-Login-9f3a"),
		RefreshToken: trans.Ptr("rt-Login-9f3a"),
	}
	createUser := &identityV1.CreateUserRequest{
		Data:     &identityV1.User{Username: trans.Ptr("bob")},
		Password: trans.Ptr("pw-Create-9f3a"),
	}
	updateUser := &identityV1.UpdateUserRequest{
		Id:       7,
		Data:     &identityV1.User{Username: trans.Ptr("carol")},
		Password: trans.Ptr("pw-Update-9f3a"),
	}
	changePassword := &identityV1.ChangePasswordRequest{
		OldPassword: "old-Change-9f3a",
		NewPassword: "new-Change-9f3a",
	}

	cases := []struct {
		name    string
		req     proto.Message
		secrets []string
		keep    []string
	}{
		{
			name:    "LoginRequest",
			req:     login,
			secrets: []string{"pw-Login-9f3a", "cs-Login-9f3a", "rt-Login-9f3a"},
			keep:    []string{"alice"},
		},
		{
			name:    "CreateUserRequest",
			req:     createUser,
			secrets: []string{"pw-Create-9f3a"},
			keep:    []string{"bob"},
		},
		{
			name:    "UpdateUserRequest",
			req:     updateUser,
			secrets: []string{"pw-Update-9f3a"},
			keep:    []string{"carol"},
		},
		{
			name:    "ChangePasswordRequest",
			req:     changePassword,
			secrets: []string{"old-Change-9f3a", "new-Change-9f3a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := proto.Clone(tc.req)
			args, seen := runAccessLog(t, tc.req)
			require.Same(t, tc.req, seen, "handler 必须拿到原始 req")
			require.True(t, proto.Equal(before, tc.req), "日志脱敏不能改原始 req")
			for _, secret := range tc.secrets {
				require.NotContains(t, args, secret)
			}
			for _, plain := range tc.keep {
				require.Contains(t, args, plain)
			}
		})
	}
}

func TestAccessLogServer_RedactsNestedPasswordWithoutAnnotation(t *testing.T) {
	req := newNestedPasswordMessage(t)
	before := proto.Clone(req)
	args, seen := runAccessLog(t, req)
	require.Same(t, req, seen)
	require.True(t, proto.Equal(before, req), "兜底清空发生在克隆上")
	for _, secret := range []string{
		"nested-pw-9f3a",
		"repeated-pw-9f3a",
		"map-msg-pw-9f3a",
		"map-str-pw-9f3a",
		"repeated-token-9f3a",
	} {
		require.NotContains(t, args, secret)
	}
	require.Contains(t, args, "dave")
	require.Contains(t, args, "keep-note")
}

func TestAccessLogServer_NonProtoKeepsKratosArgs(t *testing.T) {
	stringer := secretStringer{Password: "stringer-secret-9f3a"}
	args, seen := runAccessLog(t, stringer)
	require.Equal(t, "stringer:stringer-secret-9f3a", args)
	require.Equal(t, stringer, seen)

	redacted := secretRedacter{Password: "redacter-secret-9f3a"}
	args, seen = runAccessLog(t, redacted)
	require.Equal(t, "redacted-marker", args)
	require.NotContains(t, args, "redacter-secret-9f3a")
	require.Equal(t, redacted, seen)

	plain := struct{ Name string }{Name: "plain-name"}
	args, _ = runAccessLog(t, plain)
	require.Contains(t, args, "plain-name")
}

func TestAccessLogServer_ErrorLevelMatchesKratos(t *testing.T) {
	logger := &captureLogger{}
	mw := AccessLogServer(logger)
	ctx := transport.NewServerContext(context.Background(), &fakeTransporter{op: "/test.AccessLog/Fail"})
	_, err := mw(func(context.Context, any) (any, error) {
		return nil, errors.BadRequest("INVALID", "nope")
	})(ctx, "not-proto")
	require.Error(t, err)
	require.Equal(t, log.LevelError, logger.level)
	require.Equal(t, int32(400), kv(t, logger.keyvals, "code"))
	require.Equal(t, "INVALID", kv(t, logger.keyvals, "reason"))
	require.NotEmpty(t, kv(t, logger.keyvals, "stack"))
	require.Equal(t, "not-proto", kv(t, logger.keyvals, "args"))
}

type secretStringer struct{ Password string }

func (s secretStringer) String() string { return "stringer:" + s.Password }

type secretRedacter struct{ Password string }

func (s secretRedacter) Redact() string { return "redacted-marker" }

func newNestedPasswordMessage(t *testing.T) proto.Message {
	t.Helper()
	stringField := func(name string, number int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			JsonName: proto.String(name),
			Number:   proto.Int32(number),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		}
	}
	mapEntry := func(name, valueType string) *descriptorpb.DescriptorProto {
		value := &descriptorpb.FieldDescriptorProto{
			Name:     proto.String("value"),
			JsonName: proto.String("value"),
			Number:   proto.Int32(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
		if valueType == "string" {
			value.Type = descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
		} else {
			value.Type = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
			value.TypeName = proto.String(valueType)
		}
		return &descriptorpb.DescriptorProto{
			Name:    proto.String(name),
			Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
			Field: []*descriptorpb.FieldDescriptorProto{
				stringField("key", 1),
				value,
			},
		}
	}

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("access_log_fixture.proto"),
		Package: proto.String("accesslog.fixture.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("password", 1),
					stringField("note", 2),
				},
			},
			{
				Name: proto.String("Outer"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("username", 1),
					{
						Name:     proto.String("inner"),
						JsonName: proto.String("inner"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".accesslog.fixture.v1.Inner"),
					},
					{
						Name:     proto.String("extras"),
						JsonName: proto.String("extras"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".accesslog.fixture.v1.Inner"),
					},
					{
						Name:     proto.String("bag"),
						JsonName: proto.String("bag"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".accesslog.fixture.v1.Outer.BagEntry"),
					},
					{
						Name:     proto.String("password"),
						JsonName: proto.String("password"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".accesslog.fixture.v1.Outer.PasswordEntry"),
					},
					{
						Name:     proto.String("token"),
						JsonName: proto.String("token"),
						Number:   proto.Int32(6),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					mapEntry("BagEntry", ".accesslog.fixture.v1.Inner"),
					mapEntry("PasswordEntry", "string"),
				},
			},
		},
	}

	fd, err := protodesc.NewFile(file, nil)
	require.NoError(t, err)
	outerDesc := fd.Messages().ByName("Outer")
	innerDesc := fd.Messages().ByName("Inner")
	require.NotNil(t, outerDesc)
	require.NotNil(t, innerDesc)

	setString := func(m protoreflect.Message, name protoreflect.Name, value string) {
		t.Helper()
		fd := m.Descriptor().Fields().ByName(name)
		require.NotNil(t, fd)
		m.Set(fd, protoreflect.ValueOfString(value))
	}
	newInner := func(password, note string) protoreflect.Message {
		msg := dynamicpb.NewMessage(innerDesc).ProtoReflect()
		setString(msg, "password", password)
		if note != "" {
			setString(msg, "note", note)
		}
		return msg
	}

	outer := dynamicpb.NewMessage(outerDesc)
	setString(outer.ProtoReflect(), "username", "dave")
	outer.Set(outerDesc.Fields().ByName("inner"), protoreflect.ValueOfMessage(newInner("nested-pw-9f3a", "keep-note")))

	extras := outer.Mutable(outerDesc.Fields().ByName("extras")).List()
	extras.Append(protoreflect.ValueOfMessage(newInner("repeated-pw-9f3a", "")))

	bag := outer.Mutable(outerDesc.Fields().ByName("bag")).Map()
	bag.Set(protoreflect.ValueOfString("k").MapKey(), protoreflect.ValueOfMessage(newInner("map-msg-pw-9f3a", "")))

	passwords := outer.Mutable(outerDesc.Fields().ByName("password")).Map()
	passwords.Set(protoreflect.ValueOfString("k").MapKey(), protoreflect.ValueOfString("map-str-pw-9f3a"))

	tokens := outer.Mutable(outerDesc.Fields().ByName("token")).List()
	tokens.Append(protoreflect.ValueOfString("repeated-token-9f3a"))
	return outer
}
