package service

import (
	"context"
	"regexp"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/third_party/go-utils/stringcase"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	externalV1 "go-wind-admin/api/gen/go/external/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
)

// ExternalDemoContactService 外部数据示例：联系人（数据源 demo）。
// 外部数据不走数据权限范围；写操作照常由 API 层审计中间件记操作日志。
type ExternalDemoContactService struct {
	adminV1.ExternalDemoContactServiceHTTPServer

	log  *bLogger.Helper
	repo *data.ExternalDemoContactRepo
}

func NewExternalDemoContactService(ctx *bootstrap.Context, repo *data.ExternalDemoContactRepo) *ExternalDemoContactService {
	return &ExternalDemoContactService{
		log:  ctx.NewLoggerHelper("external-demo-contact/service/admin-service"),
		repo: repo,
	}
}

// demoAmountRe 金额：十进制字符串，最多 16 位整数、2 位小数（对应 decimal(18,2)）
var demoAmountRe = regexp.MustCompile(`^-?\d{1,16}(\.\d{1,2})?$`)

func (s *ExternalDemoContactService) List(ctx context.Context, req *paginationV1.PagingRequest) (*externalV1.ListDemoContactResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *ExternalDemoContactService) Get(ctx context.Context, req *externalV1.GetDemoContactRequest) (*externalV1.DemoContact, error) {
	if req.GetId() == "" {
		return nil, externalV1.ErrorBadRequest("id is required")
	}
	return s.repo.Get(ctx, req.GetId())
}

func (s *ExternalDemoContactService) Create(ctx context.Context, req *externalV1.CreateDemoContactRequest) (*emptypb.Empty, error) {
	if req.GetData() == nil {
		return nil, externalV1.ErrorBadRequest("invalid parameter")
	}
	if err := validateDemoContact(req.GetData(), nil); err != nil {
		return nil, err
	}
	if _, err := s.repo.Create(ctx, req.GetData()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *ExternalDemoContactService) Update(ctx context.Context, req *externalV1.UpdateDemoContactRequest) (*emptypb.Empty, error) {
	if req.GetData() == nil || req.GetId() == "" {
		return nil, externalV1.ErrorBadRequest("invalid parameter")
	}
	if err := validateDemoContact(req.GetData(), req.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, req.GetId(), req.GetData(), req.GetUpdateMask()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *ExternalDemoContactService) Delete(ctx context.Context, req *externalV1.DeleteDemoContactRequest) (*emptypb.Empty, error) {
	if req.GetId() == "" {
		return nil, externalV1.ErrorBadRequest("id is required")
	}
	if err := s.repo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// validateDemoContact 校验字段；paths 为 nil 表示新建（全部校验），否则只校验 mask 里的字段。
func validateDemoContact(d *externalV1.DemoContact, paths []string) error {
	check := func(field string) bool {
		if paths == nil {
			return true
		}
		for _, p := range paths {
			if stringcase.ToSnakeCase(p) == field {
				return true
			}
		}
		return false
	}
	if check("name") && strings.TrimSpace(d.GetName()) == "" {
		return externalV1.ErrorBadRequest("name is required")
	}
	if check("amount") && d.Amount != nil && !demoAmountRe.MatchString(d.GetAmount()) {
		return externalV1.ErrorBadRequest("amount must be a decimal with at most 2 fractional digits")
	}
	return nil
}
