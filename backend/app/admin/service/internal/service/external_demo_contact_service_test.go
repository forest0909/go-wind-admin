package service

import (
	"context"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/extdb"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

func newExternalDemoContactServiceForTest(reg *extdb.Registry) *ExternalDemoContactService {
	ctx := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	return NewExternalDemoContactService(ctx, data.NewExternalDemoContactRepo(ctx, reg))
}

// 没配 demo 源：明确的 503 EXTERNAL_SOURCE_NOT_CONFIGURED，不是 500
func TestExternalDemoContactService_NotConfigured(t *testing.T) {
	s := newExternalDemoContactServiceForTest(extdb.NewStaticRegistry())
	_, err := s.List(context.Background(), &paginationV1.PagingRequest{})
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)
	require.EqualValues(t, 503, kerrors.FromError(err).Code)
}

func TestExternalDemoContactService_Validation(t *testing.T) {
	s := newExternalDemoContactServiceForTest(extdb.NewStaticRegistry())
	ctx := context.Background()
	name, blank, badAmount := "x", "  ", "1.234"

	_, err := s.Create(ctx, &externalV1.CreateDemoContactRequest{})
	require.True(t, externalV1.IsBadRequest(err), "nil data: %v", err)
	_, err = s.Create(ctx, &externalV1.CreateDemoContactRequest{Data: &externalV1.DemoContact{Name: &blank}})
	require.True(t, externalV1.IsBadRequest(err), "blank name: %v", err)
	_, err = s.Create(ctx, &externalV1.CreateDemoContactRequest{Data: &externalV1.DemoContact{Name: &name, Amount: &badAmount}})
	require.True(t, externalV1.IsBadRequest(err), "3 fractional digits: %v", err)

	_, err = s.Update(ctx, &externalV1.UpdateDemoContactRequest{Id: "x", Data: &externalV1.DemoContact{Amount: &badAmount},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"amount"}}})
	require.True(t, externalV1.IsBadRequest(err), "amount in mask: %v", err)
	// name 不在 mask 里时不校验 name，走到仓储才因未配置失败
	_, err = s.Update(ctx, &externalV1.UpdateDemoContactRequest{Id: "x", Data: &externalV1.DemoContact{},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"remark"}}})
	require.True(t, externalV1.IsExternalSourceNotConfigured(err), "got %v", err)

	_, err = s.Get(ctx, &externalV1.GetDemoContactRequest{})
	require.True(t, externalV1.IsBadRequest(err), "empty id: %v", err)
	_, err = s.Delete(ctx, &externalV1.DeleteDemoContactRequest{})
	require.True(t, externalV1.IsBadRequest(err), "empty id: %v", err)
}
