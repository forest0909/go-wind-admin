package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-crud/pagination"
	"go-wind-admin/third_party/go-utils/sliceutil"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/middleware/auth"
)

type AdminPortalService struct {
	adminV1.AdminPortalServiceHTTPServer

	log *bLogger.Helper

	menuRepo       *data.MenuRepo
	roleRepo       *data.RoleRepo
	userRepo       data.UserRepo
	permissionRepo *data.PermissionRepo
}

func NewAdminPortalService(
	ctx *bootstrap.Context,
	menuRepo *data.MenuRepo,
	roleRepo *data.RoleRepo,
	userRepo data.UserRepo,
	permissionRepo *data.PermissionRepo,
) *AdminPortalService {
	return &AdminPortalService{
		log:            ctx.NewLoggerHelper("admin-portal/service/admin-service"),
		menuRepo:       menuRepo,
		roleRepo:       roleRepo,
		userRepo:       userRepo,
		permissionRepo: permissionRepo,
	}
}

func (s *AdminPortalService) menuListToQueryString(menus []uint32, onlyButton bool) string {
	var ids []string
	for _, menu := range menus {
		ids = append(ids, fmt.Sprintf("\"%d\"", menu))
	}
	idsStr := fmt.Sprintf("[%s]", strings.Join(ids, ", "))
	query := map[string]string{"id__in": idsStr}

	if onlyButton {
		query["type"] = permissionV1.Menu_BUTTON.String()
	} else {
		query["type__not"] = permissionV1.Menu_BUTTON.String()
	}

	query["status"] = "ON"

	queryStr, err := json.Marshal(query)
	if err != nil {
		return ""
	}

	return string(queryStr)
}

// queryMultipleRolesMenusByRoleCodes 使用RoleCodes查询菜单，即多个角色的菜单
func (s *AdminPortalService) queryMultipleRolesMenusByRoleCodes(ctx context.Context, roleIDs []uint32) ([]uint32, error) {
	var menuIDs []uint32
	var err error
	menuIDs, err = s.roleRepo.GetRolesPermissionMenuIDs(ctx, roleIDs)
	if err != nil {
		return nil, adminV1.ErrorInternalServerError("query roles menuIDs failed")
	}

	s.log.Infof(ctx, "queryMultipleRolesMenusByRoleCodes menuIDs: %+v", menuIDs)

	menuIDs = sliceutil.Unique(menuIDs)

	return menuIDs, nil
}
func (s *AdminPortalService) GetMyPermissionCode(ctx context.Context, _ *emptypb.Empty) (*adminV1.ListPermissionCodeResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// 平台管理员与授权引擎一致，获得全部启用编码 / Match platform admin authorization.
	if operator.GetIsPlatformAdmin() {
		codes, err := s.permissionRepo.GetEnabledPermissionCodes(ctx)
		if err != nil {
			return nil, err
		}
		return &adminV1.ListPermissionCodeResponse{Codes: codes, HiddenFields: operator.GetHiddenFields()}, nil
	}

	user, err := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
		QueryBy: &identityV1.GetUserRequest_Id{
			Id: operator.UserId,
		},
	})
	if err != nil {
		s.log.Errorf(ctx, "query user failed[%s]", err.Error())
		return nil, adminV1.ErrorInternalServerError("query user failed")
	}

	roleIDs, err := s.roleRepo.EnabledRoleIDs(ctx, user.GetRoleIds())
	if err != nil {
		return nil, err
	}
	permissionIDs, err := s.roleRepo.ListPermissionIDsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}

	var permissionCodes []string
	permissionCodes, err = s.permissionRepo.GetPermissionCodesByIDs(ctx, permissionIDs)
	if err != nil {
		return nil, err
	}

	return &adminV1.ListPermissionCodeResponse{
		Codes:        permissionCodes,
		HiddenFields: operator.GetHiddenFields(),
	}, nil
}

func (s *AdminPortalService) fillRouteItem(menus []*permissionV1.Menu) []*permissionV1.MenuRouteItem {
	if len(menus) == 0 {
		return nil
	}

	var routers []*permissionV1.MenuRouteItem

	for _, v := range menus {
		if v.GetStatus() != permissionV1.Menu_ON {
			continue
		}
		if v.GetType() == permissionV1.Menu_BUTTON {
			continue
		}

		item := &permissionV1.MenuRouteItem{
			Path:      v.Path,
			Component: v.Component,
			Name:      v.Name,
			Redirect:  v.Redirect,
			Alias:     v.Alias,
			Meta:      v.Meta,
		}

		if len(v.Children) > 0 {
			item.Children = s.fillRouteItem(v.Children)
		}

		routers = append(routers, item)
	}

	return routers
}

// GetInitialContext 进入后台时一次取回菜单树、权限码和隐藏字段，内容与
// GetNavigation + GetMyPermissionCode 分别调用完全一致。
func (s *AdminPortalService) GetInitialContext(ctx context.Context, _ *emptypb.Empty) (*adminV1.InitialContextResponse, error) {
	nav, err := s.GetNavigation(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	codes, err := s.GetMyPermissionCode(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return &adminV1.InitialContextResponse{
		Menus:        nav.GetItems(),
		Permissions:  codes.GetCodes(),
		HiddenFields: codes.GetHiddenFields(),
	}, nil
}

func (s *AdminPortalService) GetNavigation(ctx context.Context, _ *emptypb.Empty) (*adminV1.ListRouteResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// 平台管理员取全部启用非按钮菜单；普通角色仅补祖先目录 / Include ancestors only.
	var roleMenus []uint32
	if !operator.GetIsPlatformAdmin() {
		user, err := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
			QueryBy: &identityV1.GetUserRequest_Id{Id: operator.UserId},
		})
		if err != nil {
			s.log.Errorf(ctx, "query user failed[%s]", err.Error())
			return nil, adminV1.ErrorInternalServerError("query user failed")
		}
		roleIDs, err := s.roleRepo.EnabledRoleIDs(ctx, user.GetRoleIds())
		if err != nil {
			return nil, err
		}
		roleMenus, err = s.queryMultipleRolesMenusByRoleCodes(ctx, roleIDs)
		if err != nil {
			return nil, err
		}
	}
	menuList, err := s.menuRepo.List(ctx, &paginationV1.PagingRequest{
		NoPaging:      trans.Ptr(true),
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"status":"ON","type__not":"BUTTON"}`},
	}, false)
	if err != nil {
		s.log.Errorf(ctx, "list route failed [%s]", err.Error())
		return nil, adminV1.ErrorInternalServerError("list route failed")
	}
	if !operator.GetIsPlatformAdmin() {
		menuList.Items = navigationMenusWithAncestors(menuList.Items, roleMenus)
	}
	menuList.Items = pagination.BuildTree(menuList.Items,
		func(m *permissionV1.Menu) *uint32 { return m.Id },
		func(m *permissionV1.Menu) *uint32 { return m.ParentId },
		func(m *permissionV1.Menu) *[]*permissionV1.Menu { return &m.Children },
	)

	return &adminV1.ListRouteResponse{Items: s.fillRouteItem(menuList.Items)}, nil
}

// navigationMenusWithAncestors 只补启用祖先目录，不扩展兄弟页面 / No sibling grants.
func navigationMenusWithAncestors(menus []*permissionV1.Menu, granted []uint32) []*permissionV1.Menu {
	byID := make(map[uint32]*permissionV1.Menu, len(menus))
	for _, m := range menus {
		byID[m.GetId()] = m
	}
	selected := make(map[uint32]bool)
	for _, id := range granted {
		chain := make(map[uint32]bool)
		m := byID[id]
		valid := m != nil
		for m != nil {
			if chain[m.GetId()] {
				valid = false
				break
			}
			chain[m.GetId()] = true
			if m.GetParentId() == 0 {
				break
			}
			m = byID[m.GetParentId()]
			if m == nil || m.GetType() != permissionV1.Menu_CATALOG {
				valid = false
				break
			}
		}
		if valid {
			for ancestor := range chain {
				selected[ancestor] = true
			}
		}
	}
	result := make([]*permissionV1.Menu, 0, len(selected))
	for _, m := range menus {
		if selected[m.GetId()] {
			result = append(result, m)
		}
	}
	return result
}
